package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Agent struct {
	connManager *ConnectionManager
	workerPool  *WorkerPool
	heartbeat   *Heartbeat
	config      *Config
}

func NewAgent(config *Config) *Agent {
	return &Agent{
		config: config,
	}
}

func (a *Agent) Start() error {
	// 生成节点队列名称
	nodeQueueName := fmt.Sprintf("cmd.node.%s", a.config.Hostname)

	// 1. 初始化连接管理器
	connManager, err := NewConnectionManager(a.config.RabbitMQURL)
	if err != nil {
		return fmt.Errorf("failed to create connection manager: %v", err)
	}
	a.connManager = connManager

	// 2. 初始化工作池
	a.workerPool = NewWorkerPool(a.config.MaxConcurrentTasks)

	// 3. 初始化心跳
	a.heartbeat = NewHeartbeat(connManager, a.config.Hostname, 10*time.Second)

	// 4. 启动连接管理器
	if err := connManager.Start(); err != nil {
		return fmt.Errorf("failed to start connection manager: %v", err)
	}

	// 5. 启动心跳
	a.heartbeat.Start()

	// 6. 启动工作池
	a.workerPool.Start()

	// 7. 声明交换机
	// 命令交换机
	if err := connManager.DeclareExchange("sys_cmd_exchange", "topic"); err != nil {
		return fmt.Errorf("failed to declare exchange: %v", err)
	}
	// 结果交换机
	if err := connManager.DeclareExchange("sys_result_exchange", "topic"); err != nil {
		return fmt.Errorf("failed to declare result exchange: %v", err)
	}
	// 心跳交换机
	if err := connManager.DeclareExchange("sys_monitor_exchange", "topic"); err != nil {
		return fmt.Errorf("failed to declare monitor exchange: %v", err)
	}

	// 8. 绑定队列
	// 单机队列
	if err := connManager.BindQueue(
		nodeQueueName,
		"sys_cmd_exchange",
		fmt.Sprintf("cmd.node.%s", a.config.Hostname),
		a.handleMessage,
	); err != nil {
		return fmt.Errorf("failed to bind node queue: %v", err)
	}

	// 广播队列：将私有队列绑定到 cmd.all 路由键
	if err := connManager.BindQueue(
		nodeQueueName,
		"sys_cmd_exchange",
		"cmd.all",
		a.handleMessage,
	); err != nil {
		return fmt.Errorf("failed to bind broadcast queue: %v", err)
	}

	// 分组队列：将私有队列绑定到分组路由键
	if a.config.Group != "" {
		if err := connManager.BindQueue(
			nodeQueueName,
			"sys_cmd_exchange",
			fmt.Sprintf("cmd.group.%s", a.config.Group),
			a.handleMessage,
		); err != nil {
			return fmt.Errorf("failed to bind group queue: %v", err)
		}
	}

	log.Println("Agent started successfully")
	return nil
}

func (a *Agent) handleMessage(msg []byte) {
	a.workerPool.Submit(func() {
		executor := NewExecutor(a.config, a.connManager)
		if err := executor.Execute(msg); err != nil {
			log.Printf("Error executing task: %v", err)
		}
	})
}

func (a *Agent) Stop() {
	if a.heartbeat != nil {
		a.heartbeat.Stop()
	}

	if a.workerPool != nil {
		a.workerPool.Stop()
	}

	if a.connManager != nil {
		a.connManager.Stop()
	}

	log.Println("Agent stopped")
}

func main() {
	// 加载配置
	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	agent := NewAgent(config)

	// 启动 Agent
	if err := agent.Start(); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}

	// 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// 停止 Agent
	agent.Stop()
}
