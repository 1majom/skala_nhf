package main

import (
    "database/sql"
    "encoding/json"
    "log"
    "os"
    "time"

    _ "github.com/lib/pq"
    amqp "github.com/rabbitmq/amqp091-go"  
)

type Order struct {
	TableNumber int         `json:"table_number"`
	Items       []OrderItem `json:"items"`
	TotalPrice  float64     `json:"total_price"`
}

type OrderItem struct {
	MenuItemID int `json:"menu_item_id"`
	Quantity   int `json:"quantity"`
}

type CompletedOrder struct {
	OrderDate time.Time `json:"order_date"`
	Items     string    `json:"items"`
	Price     float64   `json:"price"`
}

func main() {
	log.Printf("Starting chef service...")

	var conn *amqp.Connection
	var err error
	maxRetries := 5

	for i := 0; i < maxRetries; i++ {
		amqpAddr := os.Getenv("AMQP_ADDR")
		if amqpAddr == "" {
			amqpAddr = "amqp://guest:guest@rabbitmq:5672/"
			log.Printf("No AMQP_ADDR provided, using default: %s", amqpAddr)
		}

		log.Printf("Attempting to connect to RabbitMQ at: %s", amqpAddr)
		conn, err = amqp.Dial(amqpAddr)
		if err == nil {
			log.Printf("Successfully connected to RabbitMQ")
			break
		}

		log.Printf("Failed to connect to RabbitMQ (attempt %d/%d): %v", i+1, maxRetries, err)
		if i < maxRetries-1 {
			log.Printf("Retrying in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}

	if err != nil {
		log.Fatal("Failed to connect to RabbitMQ after retries:", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatal("Failed to open channel:", err)
	}
	defer ch.Close()


	log.Printf("Declaring orders queue...")
	q, err := ch.QueueDeclare(
		"orders", // queue name
		false,    // durable
		false,    // delete when unused
		false,    // exclusive
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		log.Fatal("Failed to declare queue:", err)
	}
	log.Printf("Queue 'orders' declared successfully")

	// Connect to PostgreSQL
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://restaurant:devpassword@postgres:5432/restaurant?sslmode=disable"
		log.Printf("No DATABASE_URL provided, using default: %s", dbURL)
	}
	
	log.Printf("Connecting to database...")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Test database connection
	err = db.Ping()
	if err != nil {
		log.Fatal("Failed to ping database:", err)
	}
	log.Printf("Successfully connected to database")

	// Create table if not exists
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS completed_orders (
			id SERIAL PRIMARY KEY,
			order_date TIMESTAMP NOT NULL,
			items TEXT NOT NULL,
			price DECIMAL NOT NULL
		)
	`)
	if err != nil {
		log.Fatal("Failed to create table:", err)
	}
	log.Printf("Ensured completed_orders table exists")

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		false,  // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	if err != nil {
		log.Fatal("Failed to register a consumer:", err)
	}

	log.Printf("Chef is waiting for orders...")

	forever := make(chan bool)

	go func() {
		for d := range msgs {
			log.Printf("Received an order: %s", d.Body)

			var order Order
			if err := json.Unmarshal(d.Body, &order); err != nil {
				log.Printf("Error parsing order: %v", err)
				d.Nack(false, true) // Negative acknowledge, requeue
				continue
			}

			log.Printf("Processing order for table %d with %d items",
				order.TableNumber, len(order.Items))

			// Simulate cooking time (10 seconds)
			time.Sleep(10 * time.Second)

			// Save to database
			itemsJSON, _ := json.Marshal(order.Items)
			result, err := db.Exec(`
				INSERT INTO completed_orders (order_date, items, price)
				VALUES ($1, $2, $3)
			`, time.Now(), string(itemsJSON), order.TotalPrice)

			if err != nil {
				log.Printf("Error saving completed order: %v", err)
				d.Nack(false, true) // Negative acknowledge, requeue
				continue
			}

			rows, _ := result.RowsAffected()
			log.Printf("Successfully saved order. Rows affected: %d", rows)
			
			d.Ack(false) // Acknowledge message
		}
	}()

	<-forever
}