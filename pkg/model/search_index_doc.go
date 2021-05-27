package model

type SearchIndexDoc struct {
	ID      string
	Fields  map[string]interface{}
	Options map[string]SearchIndexOption
}

type SearchIndexOption struct {
	// Boost, a number that specifies the relevance in search results.
	// Content that is indexed with a boost value greater than
	// 1 is more relevant than content that is indexed without
	// a boost value. Content with a boost value less than one
	// is not so relevant. Value is a positive floating point
	// number. Default is 1 (no boosting).
	Boost int `json:"boost"`

	// Facet, creates a faceted index. See Faceting. Values are true
	// or false. Default is false.
	Facet bool `json:"facet"`

	// Index, whether the data is indexed, and if so, how. If set to
	// false, the data cannot be used for searches, but can still be
	// retrieved from the index if store is set to true. See Analyzers.
	// Values are true or false. Default is true
	Index *bool `json:"index"`

	// Store, if true, the value is returned in the search result;
	// otherwise, the value is not returned. Values are true or false.
	// Default is false.
	Store bool `json:"store"`
}

// Returns true when the data should be indexed
func (o SearchIndexOption) ShouldIndex() bool {
	if o.Index == nil {
		return true // default value
	}
	return *o.Index
}
