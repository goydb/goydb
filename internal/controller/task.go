package controller

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
)

const taskProcessCount = 10

type Task struct {
	Storage *storage.Storage
}

func (c Task) Run(ctx context.Context) {
	t := time.NewTicker(time.Millisecond * 500)
	for range t.C {
		err := c.ProcessAllTasks(ctx)
		if err != nil {
			log.Printf("Failed processing of all tasks: %v", err)
		}
	}
}

func (c Task) ProcessAllTasks(ctx context.Context) error {
	dbs, err := c.Storage.Databases(ctx)
	if err != nil {
		return err
	}

	for _, dbName := range dbs {
		db, err := c.Storage.Database(ctx, dbName)
		if err != nil {
			return err
		}

		err = c.ProcessTasksForDatabase(ctx, db)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c Task) ProcessTasksForDatabase(ctx context.Context, db *storage.Database) error {
	for {
		// check if context should be canceled
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		tasks, err := db.GetTasks(ctx, taskProcessCount)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			err := c.ProcessTask(ctx, task)
			if err != nil {
				log.Printf("Failed to process %s due to: %v", task, err)
			}
		}
		err = db.CompleteTasks(ctx, tasks)
		if err != nil {
			return err
		}
		if len(tasks) < taskProcessCount {
			break
		}
	}

	return nil
}

func (c Task) ProcessTask(ctx context.Context, task *model.Task) error {
	db, err := c.Storage.Database(ctx, task.DBName)
	if err != nil {
		return err
	}
	vc := DesignDoc{
		DB: db,
	}

	idx, ok := db.Indices()[task.DesignDocFn]
	if !ok {
		return fmt.Errorf("failed to update index %q doesn't exist", task.DesignDocFn)
	}

	switch task.Action {
	case model.ActionUpdateView:
		err = vc.Rebuild(ctx, task, idx)
	default:
		err = fmt.Errorf("unknown task action: %d", task.Action)
	}
	if err != nil {
		return err
	}

	return nil
}
