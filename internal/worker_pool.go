package internal

import (
	"log"
	"sync"
)

// WorkerPool 工作池，限制并发任务数
type WorkerPool struct {
	maxWorkers int
	taskQueue  chan func()
	wg         sync.WaitGroup
	running    bool
}

// NewWorkerPool 创建新的工作池
func NewWorkerPool(maxWorkers int) *WorkerPool {
	return &WorkerPool{
		maxWorkers: maxWorkers,
		taskQueue:  make(chan func(), 100),
	}
}

// Start 启动工作池
func (wp *WorkerPool) Start() {
	wp.running = true

	// 启动工作线程
	for i := 0; i < wp.maxWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}

	log.Printf("Worker pool started with %d workers", wp.maxWorkers)
}

// Stop 停止工作池
func (wp *WorkerPool) Stop() {
	wp.running = false
	close(wp.taskQueue)
	wp.wg.Wait()
	log.Println("Worker pool stopped")
}

// Submit 提交任务到工作池
func (wp *WorkerPool) Submit(task func()) {
	if !wp.running {
		return
	}

	wp.taskQueue <- task
}

// worker 工作线程
func (wp *WorkerPool) worker() {
	defer wp.wg.Done()

	for wp.running {
		select {
		case task, ok := <-wp.taskQueue:
			if !ok {
				return
			}
			task()
		}
	}
}
