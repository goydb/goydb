package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	bolt "go.etcd.io/bbolt"
	"gopkg.in/mgo.v2/bson"
)

var taskBucket = []byte("tasks")

func (d *Database) AddTasks(ctx context.Context, tasks []*model.Task) error {
	err := d.Transaction(ctx, func(tx port.Transaction) error {
		return d.AddTasksTx(ctx, tx, tasks)
	})
	return err
}

func (d *Database) AddTasksTx(ctx context.Context, tx port.Transaction, tasks []*model.Task) error {
	bucket, err := (tx.(*Transaction).tx).CreateBucketIfNotExists(taskBucket)
	if err != nil {
		return err
	}

	for _, task := range tasks {
		i, err := bucket.NextSequence()
		if err != nil {
			return err
		}
		key, err := cbor.Marshal(i)
		if err != nil {
			return err
		}

		task.ID = i
		data, err := bson.Marshal(task)
		if err != nil {
			return err
		}

		err = bucket.Put(key, data)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Database) GetTasks(ctx context.Context, count int) ([]*model.Task, error) {
	var tasks []*model.Task
	err := d.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(taskBucket)
		if bucket == nil {
			return nil
		}

		c := bucket.Cursor()
		i := 0
		for k, v := c.First(); k != nil && i < count; k, v = c.Next() {
			var task = new(model.Task)
			err := bson.Unmarshal(v, task)
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
			err = bucket.Put(k, data)
			if err != nil {
				return err
			}

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

	err = d.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(taskBucket)
		if bucket == nil {
			return nil
		}

		err = bucket.Put(key, data)
		if err != nil {
			return err
		}

		return nil
	})

	return err
}

func (d *Database) PeekTasks(ctx context.Context, count int) ([]*model.Task, error) {
	var tasks []*model.Task
	err := d.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(taskBucket)
		if bucket == nil {
			return nil
		}

		c := bucket.Cursor()
		i := 0
		for k, v := c.First(); k != nil && i < count; k, v = c.Next() {
			var task = new(model.Task)
			err := bson.Unmarshal(v, task)
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
	err := d.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(taskBucket)
		if bucket == nil {
			return fmt.Errorf("no bucket to delete the tasks from: %q", string(taskBucket))
		}

		for _, task := range tasks {
			key, err := cbor.Marshal(task.ID)
			if err != nil {
				return err
			}
			err = bucket.Delete(key)
			if err != nil {
				return err
			}
		}

		return nil
	})
	return err
}

func (d *Database) TaskCount(ctx context.Context) (int, error) {
	var count int
	err := d.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(taskBucket)
		if bucket == nil {
			return nil
		}

		count = bucket.Stats().KeyN
		return nil
	})
	if err != nil {
		return 0, err
	}

	return count, nil
}
