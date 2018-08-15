package storage

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/contribsys/faktory/util"
	"github.com/stretchr/testify/assert"
)

func TestBasicQueueOps(t *testing.T) {
	t.Skip()
	t.Run("Push", func(t *testing.T) {
		t.Parallel()

		store, err := OpenRedis()
		assert.NoError(t, err)
		defer store.Close()
		q, err := store.GetQueue("default")
		assert.NoError(t, err)

		assert.EqualValues(t, 0, q.Size())

		data, err := q.Pop()
		assert.NoError(t, err)
		assert.Nil(t, data)

		err = q.Push(5, []byte("hello"))
		assert.NoError(t, err)
		assert.EqualValues(t, 1, q.Size())

		err = q.Push(5, []byte("world"))
		assert.NoError(t, err)
		assert.EqualValues(t, 2, q.Size())

		values := [][]byte{
			[]byte("hello"),
			[]byte("world"),
		}
		q.Each(func(idx int, key, value []byte) error {
			assert.Equal(t, values[idx], value)
			return nil
		})

		data, err = q.Pop()
		assert.NoError(t, err)
		assert.Equal(t, []byte("hello"), data)
		assert.EqualValues(t, 1, q.Size())

		cnt, err := q.Clear()
		assert.NoError(t, err)
		assert.EqualValues(t, 1, cnt)
		assert.EqualValues(t, 0, q.Size())

		// valid names:
		_, err = store.GetQueue("A-Za-z0-9_.-")
		assert.NoError(t, err)
		_, err = store.GetQueue("-")
		assert.NoError(t, err)
		_, err = store.GetQueue("A")
		assert.NoError(t, err)
		_, err = store.GetQueue("a")
		assert.NoError(t, err)

		// invalid names:
		_, err = store.GetQueue("default?page=1")
		assert.Error(t, err)
		_, err = store.GetQueue("user@example.com")
		assert.Error(t, err)
		_, err = store.GetQueue("c&c")
		assert.Error(t, err)
		_, err = store.GetQueue("priority|high")
		assert.Error(t, err)
		_, err = store.GetQueue("")
		assert.Error(t, err)
	})

	t.Run("priority", func(t *testing.T) {
		store, err := OpenRedis()
		assert.NoError(t, err)
		defer store.Close()
		q, err := store.GetQueue("default")
		assert.NoError(t, err)

		assert.EqualValues(t, 0, q.Size())

		n := 100
		// Push N jobs to queue with low priority
		// Get Size() each time
		for i := 0; i < n; i++ {
			err = q.Push(1, []byte("1"))
			assert.NoError(t, err)
			assert.EqualValues(t, i+1, q.Size())
		}

		// Push N jobs to queue with high priority
		// Get Size() each time
		for i := 0; i < n; i++ {
			err = q.Push(3, []byte("3"))
			assert.NoError(t, err)
			assert.EqualValues(t, i+1+n, q.Size())
		}

		// Push N jobs to queue with medium priority
		// Get Size() each time
		for i := 0; i < n; i++ {
			err = q.Push(2, []byte("2"))
			assert.NoError(t, err)
			assert.EqualValues(t, i+1+2*n, q.Size())
		}

		if !assert.EqualValues(t, 3*n, q.Size()) {
			return
		}

		for i := 0; i < n; i++ {
			data, err := q.Pop()
			assert.NoError(t, err)
			assert.Equal(t, []byte("3"), data)
			assert.EqualValues(t, 3*n-(i+1), q.Size())
		}

		for i := 0; i < n; i++ {
			data, err := q.Pop()
			assert.NoError(t, err)
			assert.Equal(t, []byte("2"), data)
			assert.EqualValues(t, 2*n-(i+1), q.Size())
		}

		for i := 0; i < n; i++ {
			data, err := q.Pop()
			assert.NoError(t, err)
			assert.Equal(t, []byte("1"), data)
			assert.EqualValues(t, n-(i+1), q.Size())
		}

		// paging starting with empty queue

		err = q.Push(1, []byte("a"))
		assert.NoError(t, err)
		err = q.Push(2, []byte("b"))
		assert.NoError(t, err)
		err = q.Push(3, []byte("c"))
		assert.NoError(t, err)

		// make sure we're paging with priority in mind
		expectations := []struct {
			value    []byte
			index    int
			sequence uint64
			priority uint8
		}{
			{[]byte("c"), 0, 1, 3},
			{[]byte("b"), 1, 1, 2},
			{[]byte("a"), 2, 1, 1},
		}
		count := 0
		err = q.Page(0, 3, func(index int, k, v []byte) error {
			assert.Equal(t, expectations[count].index, index)
			//_, priority, seq := decodeKey(q.Name(), k)
			//assert.Equal(t, expectations[count].priority, priority)
			//assert.Equal(t, expectations[count].sequence, seq)
			//assert.Equal(t, expectations[count].value, v)
			count++
			return nil
		})
		assert.NoError(t, err)
	})

	t.Run("heavy", func(t *testing.T) {
		t.Parallel()

		store, err := OpenRedis()
		assert.NoError(t, err)
		defer store.Close()
		q, err := store.GetQueue("default")
		assert.NoError(t, err)

		assert.EqualValues(t, 0, q.Size())
		err = q.Push(5, []byte("first"))
		assert.NoError(t, err)
		n := 5000
		// Push N jobs to queue
		// Get Size() each time
		for i := 0; i < n; i++ {
			_, data := fakeJob()
			err = q.Push(5, data)
			assert.NoError(t, err)
			assert.EqualValues(t, i+2, q.Size())
		}

		err = q.Push(5, []byte("last"))
		assert.NoError(t, err)
		assert.EqualValues(t, n+2, q.Size())

		q, err = store.GetQueue("default")
		assert.NoError(t, err)

		// Pop N jobs from queue
		// Get Size() each time
		assert.EqualValues(t, n+2, q.Size())
		data, err := q.Pop()
		assert.NoError(t, err)
		assert.Equal(t, []byte("first"), data)
		for i := 0; i < n; i++ {
			_, err := q.Pop()
			assert.NoError(t, err)
			assert.EqualValues(t, n-i, q.Size())
		}
		data, err = q.Pop()
		assert.NoError(t, err)
		assert.Equal(t, []byte("last"), data)
		assert.EqualValues(t, 0, q.Size())

		data, err = q.Pop()
		assert.NoError(t, err)
		assert.Nil(t, data)
	})

	t.Run("threaded", func(t *testing.T) {
		t.Parallel()
		store, err := OpenRedis()
		assert.NoError(t, err)
		defer store.Close()
		q, err := store.GetQueue("default")
		assert.NoError(t, err)

		tcnt := 5
		n := 1000

		var wg sync.WaitGroup
		for i := 0; i < tcnt; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				pushAndPop(t, n, q)
			}()
		}

		wg.Wait()
		assert.EqualValues(t, 0, counter)
		assert.EqualValues(t, 0, q.Size())

		q.Each(func(idx int, k, v []byte) error {
			atomic.AddInt64(&counter, 1)
			//log.Println(string(k), string(v))
			return nil
		})
		assert.EqualValues(t, 0, counter)
	})
}

var (
	counter int64
)

func pushAndPop(t *testing.T, n int, q Queue) {
	for i := 0; i < n; i++ {
		_, data := fakeJob()
		err := q.Push(5, data)
		assert.NoError(t, err)
		atomic.AddInt64(&counter, 1)
	}

	for i := 0; i < n; i++ {
		value, err := q.Pop()
		assert.NoError(t, err)
		assert.NotNil(t, value)
		atomic.AddInt64(&counter, -1)
	}
}

func fakeJob() (string, []byte) {
	return fakeJobWithPriority(5)
}

func fakeJobWithPriority(priority uint64) (string, []byte) {
	jid := util.RandomJid()
	nows := util.Nows()
	return jid, []byte(fmt.Sprintf(`{"jid":"%s","created_at":"%s","priority":%d,"queue":"default","args":[1,2,3],"class":"SomeWorker"}`, jid, nows, priority))
}
