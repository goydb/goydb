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

	Ddfn   string
	DBName string

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
	b.WriteString(" ddfn=\"")
	b.WriteString(t.Ddfn)
	b.WriteString("\"")
	b.WriteString(">")
	return b.String()
}
