package model

import (
	"strconv"
	"strings"
	"time"
)

type TaskAction int

const (
	ActionUpdateView TaskAction = iota
)

type Task struct {
	ID          uint64
	ActiveSince time.Time
	Action      TaskAction

	ViewDocID string
	DocID     string
	DBName    string

	UpdatedAt       time.Time
	ProcessingTotal int // total number of things to process
	Processed       int // number of things processed
}

func (t Task) String() string {
	var b strings.Builder
	b.WriteString("<Task ID=")
	b.WriteString(strconv.Itoa(int(t.ID)))
	b.WriteString(" action=")
	b.WriteString(strconv.Itoa(int(t.Action)))
	b.WriteString(" db=")
	b.WriteString(t.DBName)
	if t.ViewDocID != "" {
		b.WriteString(" view=\"")
		b.WriteString(t.ViewDocID)
		b.WriteString("\"")
	}
	if t.DocID != "" {
		b.WriteString(" doc=\"")
		b.WriteString(t.DocID)
		b.WriteString("\"")
	}
	b.WriteString(">")
	return b.String()
}
