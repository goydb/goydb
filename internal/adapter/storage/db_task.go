package storage

import (
	"context"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"gopkg.in/mgo.v2/bson"
)

var taskBucket = []byte("tasks")

func (d *Database) AddTasks(ctx context.Context, tasks []*model.Task) error {
	err := d.Transaction(ctx, func(tx *Transaction) error {
		return d.AddTasksTx(ctx, tx, tasks)
	})
	return err
}

func (d *Database) AddTasksTx(ctx context.Context, tx port.EngineWriteTransaction, tasks []*model.Task) error {
	tx.EnsureBucket(taskBucket)

	for _, task := range tasks {
		data, err := bson.Marshal(task)
		if err != nil {
			return err
		}

		tx.PutWithSequence(taskBucket, nil, data, func(key []byte, i uint64) ([]byte, []byte) {
			key, err := cbor.Marshal(i)
			if err != nil {
				panic(err)
			}
			return key, nil
		})
	}

	return nil
}

func (d *Database) GetTasks(ctx context.Context, count int) ([]*model.Task, error) {
	var tasks []*model.Task
	err := d.db.WriteTransaction(func(tx port.EngineWriteTransaction) error {
		c, err := tx.Cursor(taskBucket)
		if err != nil {
			return nil
		}

		i := 0
		for k, v := c.First(); k != nil && i < count; k, v = c.Next() {
			var task = new(model.Task)
			err := bson.Unmarshal(v, task)
			if err != nil {
				return err
			}
			err = cbor.Unmarshal(k, &task.ID)
			if err != nil {
				return err
			}
			// not active since 5 minutes
			//if task.ActiveSince.Before(time.Now().Add(time.Minute * 5)) {
			task.ActiveSince = time.Now()
			data, err := bson.Marshal(task)
			if err != nil {
				return err
			}
			tx.Put(taskBucket, k, data)

			tasks = append(tasks, task)
			//}
			i++
		}

		return nil
	})
	return tasks, err
}

func (d *Database) UpdateTask(ctx context.Context, task *model.Task) error {
	task.UpdatedAt = time.Now()

	data, err := bson.Marshal(task)
	if err != nil {
		return err
	}
	key, err := cbor.Marshal(task.ID)
	if err != nil {
		return err
	}

	err = d.db.WriteTransaction(func(tx port.EngineWriteTransaction) error {
		tx.Put(taskBucket, key, data)
		return nil
	})

	return err
}

func (d *Database) PeekTasks(ctx context.Context, count int) ([]*model.Task, error) {
	var tasks []*model.Task
	err := d.db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		c, err := tx.Cursor(taskBucket)
		if err != nil {
			return nil
		}

		i := 0
		for k, v := c.First(); k != nil && i < count; k, v = c.Next() {
			var task = new(model.Task)
			err := bson.Unmarshal(v, task)
			if err != nil {
				return err
			}
			err = cbor.Unmarshal(k, &task.ID)
			if err != nil {
				return err
			}

			tasks = append(tasks, task)
			i++
		}

		return nil
	})
	return tasks, err
}

func (d *Database) CompleteTasks(ctx context.Context, tasks []*model.Task) error {
	err := d.db.WriteTransaction(func(tx port.EngineWriteTransaction) error {
		for _, task := range tasks {
			key, err := cbor.Marshal(task.ID)
			if err != nil {
				return err
			}
			tx.Delete(taskBucket, key)
		}

		return nil
	})
	return err
}

func (d *Database) TaskCount(ctx context.Context) (int, error) {
	var count int
	err := d.db.ReadTransaction(func(tx port.EngineReadTransaction) error {
		stats, err := tx.BucketStats(taskBucket)
		if err != nil {
			return nil
		}

		count = int(stats.Documents)
		return nil
	})
	if err != nil {
		return 0, err
	}

	return count, nil
}
