package internal

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Test_WorkerPool_Start_Stop 测试工作池的启动和停止
func Test_WorkerPool_Start_Stop(t *testing.T) {
	// 创建工作池
	workerPool := NewWorkerPool(2)

	// 启动工作池
	workerPool.Start()

	// 停止工作池
	workerPool.Stop()

	// 验证工作池已停止
	// 由于没有公开的状态字段，我们只能通过行为来验证
	// 这里我们可以尝试提交一个任务，它应该不会执行
	var executed bool
	workerPool.Submit(func() {
		executed = true
	})

	// 等待一段时间，确保任务有机会执行
	time.Sleep(100 * time.Millisecond)

	// 验证任务没有执行
	assert.False(t, executed)
}

// Test_WorkerPool_Submit_Correct 测试正确提交任务
func Test_WorkerPool_Submit_Correct(t *testing.T) {
	// 创建工作池
	workerPool := NewWorkerPool(2)

	// 启动工作池
	workerPool.Start()
	defer workerPool.Stop()

	// 提交任务
	var executed bool
	var wg sync.WaitGroup
	wg.Add(1)

	workerPool.Submit(func() {
		defer wg.Done()
		executed = true
		time.Sleep(50 * time.Millisecond)
	})

	// 等待任务执行完成
	wg.Wait()

	// 验证任务已执行
	assert.True(t, executed)
}

// Test_WorkerPool_ConcurrentLimit 测试并发限制
func Test_WorkerPool_ConcurrentLimit(t *testing.T) {
	// 创建工作池，最多2个并发任务
	workerPool := NewWorkerPool(2)

	// 启动工作池
	workerPool.Start()
	defer workerPool.Stop()

	// 提交3个任务，每个任务需要一些时间
	var executed1, executed2, executed3 bool
	var wg sync.WaitGroup
	wg.Add(3)

	startTime := time.Now()

	// 任务1
	workerPool.Submit(func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		executed1 = true
	})

	// 任务2
	workerPool.Submit(func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		executed2 = true
	})

	// 任务3
	workerPool.Submit(func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		executed3 = true
	})

	// 等待所有任务执行完成
	wg.Wait()

	// 计算执行时间
	executionTime := time.Since(startTime)

	// 验证所有任务都已执行
	assert.True(t, executed1)
	assert.True(t, executed2)
	assert.True(t, executed3)

	// 验证执行时间大于100ms（因为前两个任务并行执行，第三个任务需要等待）
	assert.Greater(t, executionTime, 100*time.Millisecond)

	// 验证执行时间小于300ms（因为任务是并行执行的）
	assert.Less(t, executionTime, 300*time.Millisecond)
}

// Test_WorkerPool_Border 测试边界情况
func Test_WorkerPool_Border(t *testing.T) {
	// 测试工作池大小为0的情况
	workerPool := NewWorkerPool(0)
	workerPool.Start()
	defer workerPool.Stop()

	// 提交任务
	var executed bool
	workerPool.Submit(func() {
		executed = true
	})

	// 等待一段时间
	time.Sleep(100 * time.Millisecond)

	// 验证任务没有执行（因为工作池大小为0）
	assert.False(t, executed)

	// 测试工作池大小为1的情况
	workerPool2 := NewWorkerPool(1)
	workerPool2.Start()
	defer workerPool2.Stop()

	var executed1, executed2 bool
	var wg sync.WaitGroup
	wg.Add(2)

	startTime := time.Now()

	// 任务1
	workerPool2.Submit(func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		executed1 = true
	})

	// 任务2
	workerPool2.Submit(func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		executed2 = true
	})

	// 等待所有任务执行完成
	wg.Wait()

	// 计算执行时间
	executionTime := time.Since(startTime)

	// 验证所有任务都已执行
	assert.True(t, executed1)
	assert.True(t, executed2)

	// 验证执行时间大于100ms（因为任务是串行执行的）
	assert.Greater(t, executionTime, 100*time.Millisecond)
}
