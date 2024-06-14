package main

import "sync"

type Task struct {
	ID           int
	Notification *Notification
}

type WorkerPool struct {
	WorkerCount int
	TasksChan   chan Task
	wg          sync.WaitGroup
}

func NewWorkerPool(workerCount int) *WorkerPool {
	return &WorkerPool{
		WorkerCount: workerCount,
		TasksChan:   make(chan Task),
	}
}

func (wp *WorkerPool) worker() {
	for task := range wp.TasksChan {
		task.Notification.SendToFcm(wp.WorkerCount)
		wp.wg.Done()
	}
}

func (wp *WorkerPool) Run() {
	for i := 0; i < wp.WorkerCount; i++ {
		go wp.worker()
	}
}

func (wp *WorkerPool) AddTask(task Task) {
	wp.wg.Add(1)
	wp.TasksChan <- task
}
