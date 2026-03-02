package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type DBChanges struct {
	Base
}

// FIXME: make config
const maxTimeout = time.Second * 60
const maxHeartbeat = time.Minute * 5

func (s *DBChanges) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	session, ok := Authenticator{Base: s.Base}.DB(w, r, db)
	if !ok {
		return
	}

	query := r.URL.Query()
	includeDocs := boolOption("include_docs", false, query)
	options := model.ChangesOptions{
		Since:      strings.ReplaceAll(query.Get("since"), `"`, ""),
		Limit:      int(intOption("limit", 1000, query)),
		Timeout:    durationOption("timeout", time.Millisecond, maxTimeout, query),
		Heartbeat:  durationOption("heartbeat", time.Millisecond, maxHeartbeat, query),
		Filter:     query.Get("filter"),
		View:       query.Get("view"),
		Descending: boolOption("descending", false, query),
		Style:      query.Get("style"),
	}
	// seq_interval, conflicts, attachments, att_encoding_info are accepted
	// for CouchDB API compatibility.

	if r.Method == "POST" {
		var body struct {
			DocIDs   []string               `json:"doc_ids"`
			Selector map[string]interface{} `json:"selector"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			options.DocIDs = body.DocIDs
			options.Selector = body.Selector
		}
	}

	feed := query.Get("feed")
	if feed == "" {
		feed = "normal"
	}
	options.Feed = feed

	// Route to appropriate handler based on feed type
	switch feed {
	case "normal", "longpoll":
		s.handleNormalFeed(w, r, db, &options, includeDocs, session)
	case "continuous":
		s.handleContinuousFeed(w, r, db, &options, includeDocs, session)
	case "eventsource":
		s.handleEventSourceFeed(w, r, db, &options, includeDocs, session)
	default:
		WriteError(w, http.StatusBadRequest, "invalid feed type")
	}
}

func (s *DBChanges) handleNormalFeed(w http.ResponseWriter, r *http.Request, db port.Database, options *model.ChangesOptions, includeDocs bool, session *model.Session) {
	// Setup heartbeat ticker if specified
	var heartbeatTicker *time.Ticker
	var heartbeatDone chan struct{}
	var heartbeatWg sync.WaitGroup
	if options.Heartbeat > 0 {
		// We'll start heartbeat after headers are sent
		heartbeatTicker = time.NewTicker(options.Heartbeat)
		heartbeatDone = make(chan struct{})
		defer func() {
			heartbeatTicker.Stop()
			close(heartbeatDone)
			heartbeatWg.Wait() // wait for goroutine to stop before returning
		}()
	}

	changes, pending, err := db.Changes(r.Context(), options)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Apply all filters
	changes = s.applyFilters(r.Context(), db, changes, options, r, session)

	if options.Descending {
		for i, j := 0, len(changes)-1; i < j; i, j = i+1, j-1 {
			changes[i], changes[j] = changes[j], changes[i]
		}
	}

	if includeDocs {
		err := db.EnrichDocuments(r.Context(), changes)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintln(w, `{"results":[`)

	// Start heartbeat goroutine if configured
	if heartbeatTicker != nil {
		if flusher, ok := w.(http.Flusher); ok {
			heartbeatWg.Add(1)
			go func() {
				defer heartbeatWg.Done()
				for {
					select {
					case <-heartbeatTicker.C:
						_, _ = fmt.Fprint(w, "\n")
						flusher.Flush()
					case <-heartbeatDone:
						return
					case <-r.Context().Done():
						return
					}
				}
			}()
		}
	}

	// print an empty line like couchdb
	if options.Limit == 0 {
		_, _ = fmt.Fprintln(w, "")
	}

	var lastSeq uint64
	for i, doc := range changes {
		if lastSeq < doc.LocalSeq {
			lastSeq = doc.LocalSeq
		}
		cd := &ChangeDoc{
			Seq:     strconv.FormatUint(doc.LocalSeq, 10),
			ID:      doc.ID,
			Deleted: doc.Deleted,
		}
		if options.Style == "all_docs" {
			if leaves, err := db.GetLeaves(r.Context(), doc.ID); err == nil && len(leaves) > 0 {
				cd.Changes = make([]Revisions, len(leaves))
				for li, l := range leaves {
					cd.Changes[li] = Revisions{Rev: l.Rev}
				}
			} else {
				cd.Changes = []Revisions{{Rev: doc.Rev}}
			}
		} else {
			cd.Changes = []Revisions{{Rev: doc.Rev}}
		}
		if includeDocs {
			cd.Doc = doc.Data
		}
		err := json.NewEncoder(w).Encode(cd)
		if err != nil {
			s.Logger.Warnf(r.Context(), "failed to encode change", "error", err)
			break
		}
		if i < len(changes)-1 {
			_, _ = fmt.Fprint(w, `,`)
		}
	}

	if options.Limit == 0 && lastSeq == 0 {
		lastSeq, _ = strconv.ParseUint(options.Since, 10, 64)
	}

	_, _ = fmt.Fprintln(w, `],`)
	_, _ = fmt.Fprintf(w, `"last_seq":"%d","pending":%d}`, lastSeq, pending)
}

func (s *DBChanges) handleContinuousFeed(w http.ResponseWriter, r *http.Request, db port.Database, options *model.ChangesOptions, includeDocs bool, session *model.Session) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Transfer-Encoding", "chunked")

	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ctx := r.Context()

	// Setup heartbeat if specified
	var heartbeatWg sync.WaitGroup
	if options.Heartbeat > 0 {
		heartbeatTicker := time.NewTicker(options.Heartbeat)
		heartbeatDone := make(chan struct{})
		defer func() {
			heartbeatTicker.Stop()
			close(heartbeatDone)
			heartbeatWg.Wait()
		}()

		heartbeatWg.Add(1)
		go func() {
			defer heartbeatWg.Done()
			for {
				select {
				case <-heartbeatTicker.C:
					_, _ = fmt.Fprint(w, "\n")
					flusher.Flush()
				case <-heartbeatDone:
					return
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Register persistent change listener
	changeChan := make(chan *model.Document, 10) // Buffered to avoid blocking
	listenerCtx, cancelListener := context.WithCancel(ctx)
	defer cancelListener()

	err := db.AddListener(listenerCtx, port.ChangeListenerFunc(func(ctx context.Context, doc *model.Document) error {
		select {
		case changeChan <- doc:
		case <-ctx.Done():
			return context.Canceled
		default:
			// Skip if buffer full (backpressure)
		}
		return nil
	}))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get initial batch of changes
	changes, _, err := db.Changes(ctx, options)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Apply all filters
	changes = s.applyFilters(ctx, db, changes, options, r, session)

	if includeDocs {
		if err := db.EnrichDocuments(ctx, changes); err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Stream initial changes
	for _, doc := range changes {
		if err := s.writeChangeDoc(w, doc, includeDocs); err != nil {
			s.Logger.Warnf(ctx, "failed to write change", "error", err)
			return
		}
		flusher.Flush()
		options.Since = strconv.FormatUint(doc.LocalSeq, 10)
	}

	// Setup timeout if specified and no heartbeat
	var timeoutTimer *time.Timer
	var timeoutChan <-chan time.Time
	if options.Timeout > 0 && options.Heartbeat == 0 {
		timeoutTimer = time.NewTimer(options.Timeout)
		defer timeoutTimer.Stop()
		timeoutChan = timeoutTimer.C
	}

	// Continuously stream changes
	for {
		select {
		case <-ctx.Done():
			return
		case doc := <-changeChan:
			// Always enrich the doc so that LocalSeq, Data, Rev, etc. are
			// populated from storage.  The listener notification only carries
			// the minimal document from PutDocument (LocalSeq is 0).
			{
				docs := []*model.Document{doc}
				if err := db.EnrichDocuments(ctx, docs); err != nil {
					s.Logger.Warnf(ctx, "failed to enrich document", "error", err)
					continue
				}
			}

			// Skip if before our since marker
			if options.Since != "" && options.Since != "now" {
				since, _ := strconv.ParseUint(options.Since, 10, 64)
				if doc.LocalSeq <= since {
					continue
				}
			}

			// Apply filters
			filtered := s.applyFilters(ctx, db, []*model.Document{doc}, options, r, session)
			if len(filtered) == 0 {
				continue // Document was filtered out
			}

			// Stream the change
			if err := s.writeChangeDoc(w, doc, includeDocs); err != nil {
				s.Logger.Warnf(ctx, "failed to write change", "error", err)
				return
			}
			flusher.Flush()
			options.Since = strconv.FormatUint(doc.LocalSeq, 10)

			// Reset timeout if active
			if timeoutTimer != nil {
				timeoutTimer.Reset(options.Timeout)
			}

		case <-timeoutChan:
			// Timeout expired, close connection
			return
		}
	}
}

func (s *DBChanges) handleEventSourceFeed(w http.ResponseWriter, r *http.Request, db port.Database, options *model.ChangesOptions, includeDocs bool, session *model.Session) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ctx := r.Context()

	// Setup heartbeat if specified
	var heartbeatWg sync.WaitGroup
	if options.Heartbeat > 0 {
		heartbeatTicker := time.NewTicker(options.Heartbeat)
		heartbeatDone := make(chan struct{})
		defer func() {
			heartbeatTicker.Stop()
			close(heartbeatDone)
			heartbeatWg.Wait()
		}()

		heartbeatWg.Add(1)
		go func() {
			defer heartbeatWg.Done()
			for {
				select {
				case <-heartbeatTicker.C:
					_, _ = fmt.Fprint(w, ": heartbeat\n\n")
					flusher.Flush()
				case <-heartbeatDone:
					return
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Register persistent change listener
	changeChan := make(chan *model.Document, 10)
	listenerCtx, cancelListener := context.WithCancel(ctx)
	defer cancelListener()

	err := db.AddListener(listenerCtx, port.ChangeListenerFunc(func(ctx context.Context, doc *model.Document) error {
		select {
		case changeChan <- doc:
		case <-ctx.Done():
			return context.Canceled
		default:
			// Skip if buffer full
		}
		return nil
	}))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get initial batch of changes
	changes, _, err := db.Changes(ctx, options)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Apply all filters
	changes = s.applyFilters(ctx, db, changes, options, r, session)

	if includeDocs {
		if err := db.EnrichDocuments(ctx, changes); err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Stream initial changes
	for _, doc := range changes {
		if err := s.writeEventSourceChange(w, doc, includeDocs); err != nil {
			s.Logger.Warnf(ctx, "failed to write change", "error", err)
			return
		}
		flusher.Flush()
		options.Since = strconv.FormatUint(doc.LocalSeq, 10)
	}

	// Setup timeout if specified and no heartbeat
	var timeoutTimer *time.Timer
	var timeoutChan <-chan time.Time
	if options.Timeout > 0 && options.Heartbeat == 0 {
		timeoutTimer = time.NewTimer(options.Timeout)
		defer timeoutTimer.Stop()
		timeoutChan = timeoutTimer.C
	}

	// Continuously stream changes
	for {
		select {
		case <-ctx.Done():
			return
		case doc := <-changeChan:
			// Always enrich the doc so that LocalSeq, Data, Rev, etc. are
			// populated from storage.  The listener notification only carries
			// the minimal document from PutDocument (LocalSeq is 0).
			{
				docs := []*model.Document{doc}
				if err := db.EnrichDocuments(ctx, docs); err != nil {
					s.Logger.Warnf(ctx, "failed to enrich document", "error", err)
					continue
				}
			}

			// Skip if before our since marker
			if options.Since != "" && options.Since != "now" {
				since, _ := strconv.ParseUint(options.Since, 10, 64)
				if doc.LocalSeq <= since {
					continue
				}
			}

			// Apply filters
			filtered := s.applyFilters(ctx, db, []*model.Document{doc}, options, r, session)
			if len(filtered) == 0 {
				continue
			}

			// Stream the change
			if err := s.writeEventSourceChange(w, doc, includeDocs); err != nil {
				s.Logger.Warnf(ctx, "failed to write change", "error", err)
				return
			}
			flusher.Flush()
			options.Since = strconv.FormatUint(doc.LocalSeq, 10)

			// Reset timeout if active
			if timeoutTimer != nil {
				timeoutTimer.Reset(options.Timeout)
			}

		case <-timeoutChan:
			return
		}
	}
}

// Helper to write a single change document in EventSource format
func (s *DBChanges) writeEventSourceChange(w http.ResponseWriter, doc *model.Document, includeDocs bool) error {
	cd := &ChangeDoc{
		Seq:     strconv.FormatUint(doc.LocalSeq, 10),
		ID:      doc.ID,
		Deleted: doc.Deleted,
		Changes: []Revisions{
			{Rev: doc.Rev},
		},
	}
	if includeDocs {
		cd.Doc = doc.Data
	}

	data, err := json.Marshal(cd)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

// Helper to write a single change document
func (s *DBChanges) writeChangeDoc(w http.ResponseWriter, doc *model.Document, includeDocs bool) error {
	cd := &ChangeDoc{
		Seq:     strconv.FormatUint(doc.LocalSeq, 10),
		ID:      doc.ID,
		Deleted: doc.Deleted,
		Changes: []Revisions{
			{Rev: doc.Rev},
		},
	}
	if includeDocs {
		cd.Doc = doc.Data
	}
	return json.NewEncoder(w).Encode(cd)
}

// applyFilters applies all configured filters to a slice of documents
func (s *DBChanges) applyFilters(ctx context.Context, db port.Database, changes []*model.Document, options *model.ChangesOptions, r *http.Request, session *model.Session) []*model.Document {
	// Apply doc_ids filter when present
	if len(options.DocIDs) > 0 {
		changes = s.filterByDocIDs(changes, options.DocIDs)
	}

	switch {
	case options.Filter == "_selector" && options.Selector != nil:
		changes = s.filterBySelector(ctx, changes, options.Selector)
	case options.Filter == "_design":
		changes = s.filterByDesign(changes)
	case options.Filter == "_view" && options.View != "":
		changes = s.filterByView(ctx, db, changes, options.View)
	case options.Filter != "" && options.Filter != "_doc_ids" && options.Filter != "_selector":
		changes = s.filterByCustomFilter(ctx, db, changes, options.Filter, r, session)
	}

	return changes
}

func (s *DBChanges) filterByDesign(changes []*model.Document) []*model.Document {
	filtered := make([]*model.Document, 0)
	for _, doc := range changes {
		if strings.HasPrefix(doc.ID, "_design/") {
			filtered = append(filtered, doc)
		}
	}
	return filtered
}

func (s *DBChanges) filterByDocIDs(changes []*model.Document, docIDs []string) []*model.Document {
	allowed := make(map[string]bool, len(docIDs))
	for _, id := range docIDs {
		allowed[id] = true
	}
	filtered := changes[:0]
	for _, doc := range changes {
		if allowed[doc.ID] {
			filtered = append(filtered, doc)
		}
	}
	return filtered
}

func (s *DBChanges) filterBySelector(ctx context.Context, changes []*model.Document, selectorMap map[string]interface{}) []*model.Document {
	// Parse selector using existing FindQuery infrastructure
	fq := model.FindQuery{}
	selectorJSON, _ := json.Marshal(map[string]interface{}{"selector": selectorMap})
	if err := json.Unmarshal(selectorJSON, &fq); err != nil {
		s.Logger.Warnf(ctx, "invalid selector", "error", err)
		return changes
	}

	// Filter documents using SelectorQuery.Match()
	filtered := make([]*model.Document, 0, len(changes))
	for _, doc := range changes {
		if matches, err := fq.Selector.Match(doc); err == nil && matches {
			filtered = append(filtered, doc)
		}
	}

	return filtered
}

func (s *DBChanges) filterByView(ctx context.Context, db port.Database, changes []*model.Document, viewPath string) []*model.Document {
	// Parse view path: accepts both "ddoc/viewname" and "_design/ddoc/viewname"
	path := strings.TrimPrefix(viewPath, "_design/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		s.Logger.Warnf(ctx, "invalid view path", "path", viewPath)
		return changes
	}
	designDocID := "_design/" + parts[0]
	viewName := parts[1]

	// Load design doc
	ddoc, err := db.GetDocument(ctx, designDocID)
	if err != nil {
		s.Logger.Warnf(ctx, "design doc not found", "id", designDocID)
		return changes
	}

	// Get view definition
	view, ok := ddoc.View(viewName)
	if !ok {
		s.Logger.Warnf(ctx, "view not found", "name", viewName)
		return changes
	}

	// Get view server builder
	builder := db.ViewEngine(ddoc.Language())
	if builder == nil {
		s.Logger.Warnf(ctx, "view engine not found", "language", ddoc.Language())
		return changes
	}

	viewServer, err := builder(view.MapFn)
	if err != nil {
		s.Logger.Warnf(ctx, "failed to compile view", "error", err)
		return changes
	}

	// Execute view for each document and include if it emits
	filtered := make([]*model.Document, 0, len(changes))
	for _, doc := range changes {
		results, err := viewServer.ExecuteView(ctx, []*model.Document{doc})
		if err == nil && len(results) > 0 {
			// Document emitted something, include it
			filtered = append(filtered, doc)
		}
	}

	return filtered
}

func (s *DBChanges) filterByCustomFilter(ctx context.Context, db port.Database, changes []*model.Document, filterPath string, r *http.Request, session *model.Session) []*model.Document {
	// Parse filter path: accepts both "ddoc/filtername" and "_design/ddoc/filtername"
	path := strings.TrimPrefix(filterPath, "_design/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		s.Logger.Warnf(ctx, "invalid filter path", "path", filterPath)
		return changes
	}
	designDocID := "_design/" + parts[0]
	filterName := parts[1]

	// Load design doc
	ddoc, err := db.GetDocument(ctx, designDocID)
	if err != nil {
		s.Logger.Warnf(ctx, "design doc not found", "id", designDocID)
		return changes
	}

	// Get filter function
	filter, ok := ddoc.Filter(filterName)
	if !ok {
		s.Logger.Warnf(ctx, "filter not found", "name", filterName)
		return changes
	}

	// Get filter server builder
	builder := db.FilterEngine(ddoc.Language())
	if builder == nil {
		s.Logger.Warnf(ctx, "filter engine not found", "language", ddoc.Language())
		return changes
	}

	// Compile filter
	filterServer, err := builder(filter.FilterFn)
	if err != nil {
		s.Logger.Warnf(ctx, "failed to compile filter", "error", err)
		return changes
	}

	// Prepare request context with session info
	sessionForCtx := session
	if sessionForCtx == nil {
		sessionForCtx = &model.Session{}
	}
	req := map[string]interface{}{
		"query":   make(map[string]interface{}),
		"userCtx": userCtxToJS(sessionForCtx, db.Name()),
	}

	// Add all query parameters to req.query
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			req["query"].(map[string]interface{})[key] = values[0]
		}
	}

	// Execute filter for each document
	filtered := make([]*model.Document, 0, len(changes))
	for _, doc := range changes {
		passed, err := filterServer.ExecuteFilter(ctx, doc, req)
		if err != nil {
			s.Logger.Warnf(ctx, "filter execution error", "error", err)
			continue
		}
		if passed {
			filtered = append(filtered, doc)
		}
	}

	return filtered
}

type ChangeDoc struct {
	Seq     string                 `json:"seq"`
	ID      string                 `json:"id"`
	Changes []Revisions            `json:"changes"`
	Deleted bool                   `json:"deleted,omitempty"`
	Doc     map[string]interface{} `json:"doc,omitempty"`
}

type Revisions struct {
	Rev string `json:"rev"`
}
