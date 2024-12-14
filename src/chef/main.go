package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	"github.com/streadway/amqp"
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

	// Connect to RabbitMQ with retry
	var conn *amqp.Connection
	var err error
	maxRetries := 5

	for i := 0; i < maxRetries; i++ {
		amqpAddr := os.Getenv("AMQP_ADDR")
		if amqpAddr == "" {
			amqpAddr = "amqp://guest:guest@10.0.2.15:5672/"
		}

		conn, err = amqp.Dial(amqpAddr)
		if err == nil {
			break
		}

		log.Printf("Failed to connect to RabbitMQ (attempt %d/%d): %v", i+1, maxRetries, err)
		time.Sleep(5 * time.Second)
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

	q, err := ch.QueueDeclare(
		"orders",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatal("Failed to declare queue:", err)
	}

	// Connect to PostgreSQL
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://restaurant:devpassword@postgres:5432/restaurant?sslmode=disable"
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

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

	msgs, err := ch.Consume(
		q.Name,
		"",
		true,
		false,
		false,
		false,
		nil,
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
				continue
			}

			// Simulate cooking time (10 seconds)
			time.Sleep(10 * time.Second)

			// to database
			itemsJSON, _ := json.Marshal(order.Items)
			_, err = db.Exec(`
                INSERT INTO completed_orders (order_date, items, price)
                VALUES ($1, $2, $3)
            `, time.Now(), string(itemsJSON), order.TotalPrice)

			if err != nil {
				log.Printf("Error saving completed order: %v", err)
				continue
			}

			log.Printf("Order completed and saved: Table %d", order.TableNumber)
		}
	}()

	<-forever
}
