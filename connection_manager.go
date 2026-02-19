package main

import (
	"log"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

// QueueBinding 队列绑定信息
type QueueBinding struct {
	ExchangeName string
	RoutingKey   string
	Handler      func([]byte)
}

// ConnectionManager 管理与 RabbitMQ 的连接
type ConnectionManager struct {
	url             string
	conn            *amqp091.Connection
	ch              *amqp091.Channel
	mu              sync.Mutex
	running         bool
	reconnecting    bool
	messageHandlers map[string]QueueBinding
}

// NewConnectionManager 创建新的连接管理器
func NewConnectionManager(url string) (*ConnectionManager, error) {
	return &ConnectionManager{
		url:             url,
		messageHandlers: make(map[string]QueueBinding),
	}, nil
}

// Start 启动连接管理器
func (cm *ConnectionManager) Start() error {
	cm.running = true
	return cm.connect()
}

// Stop 停止连接管理器
func (cm *ConnectionManager) Stop() {
	cm.running = false
	cm.mu.Lock()
	if cm.ch != nil {
		err := cm.ch.Close()
		if err != nil {
			return
		}
	}
	if cm.conn != nil {
		err := cm.conn.Close()
		if err != nil {
			return
		}
	}
	cm.mu.Unlock()
}

// connect 建立与 RabbitMQ 的连接
func (cm *ConnectionManager) connect() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.reconnecting {
		return nil
	}

	cm.reconnecting = true
	defer func() {
		cm.reconnecting = false
	}()

	log.Println("Connecting to RabbitMQ...")

	// 建立连接
	conn, err := amqp091.Dial(cm.url)
	if err != nil {
		log.Printf("Failed to connect to RabbitMQ: %v", err)
		go cm.scheduleReconnect()
		return err
	}

	// 建立通道
	ch, err := conn.Channel()
	if err != nil {
		err := conn.Close()
		if err != nil {
			return err
		}
		log.Printf("Failed to create channel: %v", err)
		go cm.scheduleReconnect()
		return err
	}

	// 设置连接关闭通知
	go func() {
		<-conn.NotifyClose(make(chan *amqp091.Error))
		log.Println("RabbitMQ connection closed, reconnecting...")
		cm.mu.Lock()
		cm.conn = nil
		cm.ch = nil
		cm.mu.Unlock()
		if cm.running {
			err := cm.connect()
			if err != nil {
				log.Printf("Failed to reconnect to RabbitMQ: %v", err)
				return
			}
		}
	}()

	cm.conn = conn
	cm.ch = ch

	log.Println("Connected to RabbitMQ successfully")

	// 重新绑定所有队列
	for queueName, binding := range cm.messageHandlers {
		if err := cm.bindQueueInternal(queueName, binding.ExchangeName, binding.RoutingKey, binding.Handler); err != nil {
			log.Printf("Failed to rebind queue %s: %v", queueName, err)
		}
	}

	return nil
}

// scheduleReconnect 安排重新连接
func (cm *ConnectionManager) scheduleReconnect() {
	if !cm.running {
		return
	}

	time.AfterFunc(5*time.Second, func() {
		if cm.running {
			err := cm.connect()
			if err != nil {
				return
			}
		}
	})
}

// DeclareExchange 声明交换机
func (cm *ConnectionManager) DeclareExchange(exchangeName, exchangeType string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.ch == nil {
		return amqp091.ErrClosed
	}

	return cm.ch.ExchangeDeclare(
		exchangeName,
		exchangeType,
		true,  // durable
		false, // auto-deleted
		false, // internal
		false, // no-wait
		nil,   // arguments
	)
}

// BindQueue 绑定队列
func (cm *ConnectionManager) BindQueue(queueName, exchangeName, routingKey string, handler func([]byte)) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 存储队列的绑定信息
	binding := QueueBinding{
		ExchangeName: exchangeName,
		RoutingKey:   routingKey,
		Handler:      handler,
	}
	cm.messageHandlers[queueName] = binding

	if cm.ch == nil {
		return nil // 连接建立后会自动绑定
	}

	return cm.bindQueueInternal(queueName, exchangeName, routingKey, handler)
}

// bindQueueInternal 内部绑定队列方法
func (cm *ConnectionManager) bindQueueInternal(queueName, exchangeName, routingKey string, handler func([]byte)) error {
	// 声明队列
	q, err := cm.ch.QueueDeclare(
		queueName,
		false, // durable - 专属队列不需要持久化
		true,  // delete when unused - 当没有消费者时自动删除
		true,  // exclusive - 专属队列，连接关闭时自动删除
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		return err
	}

	// 绑定队列到交换机
	err = cm.ch.QueueBind(
		q.Name,
		routingKey,
		exchangeName,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	// 消费消息
	msg, err := cm.ch.Consume(
		q.Name,
		"",
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return err
	}

	// 处理消息
	go func() {
		for msg := range msg {
			handler(msg.Body)
			err := msg.Ack(false)
			if err != nil {
				log.Printf("Failed to acknowledge message: %v", err)
				return
			}
		}
	}()

	return nil
}

// Publish 发布消息
func (cm *ConnectionManager) Publish(exchange, routingKey string, msg []byte) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.ch == nil {
		return amqp091.ErrClosed
	}

	return cm.ch.Publish(
		exchange,
		routingKey,
		false,
		false,
		amqp091.Publishing{
			ContentType: "application/json",
			Body:        msg,
		},
	)
}
