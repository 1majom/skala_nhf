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

type OrderEvent struct {
    EventID     string    `json:"event_id"`
    EventType   string    `json:"event_type"`
    Timestamp   time.Time `json:"timestamp"`
    Order       Order     `json:"order"`
}

type Order struct {
    TableNumber int         `json:"table_number"`
    Items       []OrderItem `json:"items"`
    Subtotal    int         `json:"subtotal"`
}

type OrderItem struct {
    MenuItemID int `json:"menu_item_id"`
    Quantity   int `json:"quantity"`
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
            table_number INT NOT NULL,
            items TEXT NOT NULL,
            subtotal INT NOT NULL,
            paid BOOLEAN NOT NULL DEFAULT FALSE
        )
    `) 
    if err != nil {
        log.Fatal("Failed to create completed_orders table:", err)
    }

    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS processed_events (
            event_id VARCHAR PRIMARY KEY,
            processed_at TIMESTAMP NOT NULL,
            order_id INT REFERENCES completed_orders(id)
        )
    `)
    if err != nil {
        log.Fatal("Failed to create processed_events table:", err)
    }

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

            var event OrderEvent
            if err := json.Unmarshal(d.Body, &event); err != nil {
                log.Printf("Error parsing event: %v", err)
                d.Nack(false, true)
                continue
            }

            var exists bool
            err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM processed_events WHERE event_id = $1)", 
                event.EventID).Scan(&exists)
            if err == nil && exists {
                log.Printf("Event %s already processed, skipping", event.EventID)
                d.Ack(false)
                continue
            }


            // Simulate cooking time (10 seconds)
            time.Sleep(10 * time.Second)

            tx, err := db.Begin()
            if err != nil {
                log.Printf("Error starting transaction: %v", err)
                d.Nack(false, true)
                continue
            }


            // Save to completed_orders
            itemsJSON, _ := json.Marshal(event.Order.Items)

            var orderID int
            err = tx.QueryRow(`
                INSERT INTO completed_orders (order_date, table_number, items, subtotal)
                VALUES ($1, $2, $3, $4)
                RETURNING id
            `, time.Now(), event.Order.TableNumber, string(itemsJSON), event.Order.Subtotal).Scan(&orderID)

            if err != nil {
                tx.Rollback()
                log.Printf("Error saving completed order: %v", err)
                d.Nack(false, true)
                continue
            }

            // Mark event as processed
            _, err = tx.Exec(`
                INSERT INTO processed_events (event_id, processed_at, order_id)
                VALUES ($1, $2, $3)
            `, event.EventID, time.Now(), orderID)

            if err != nil {
                tx.Rollback()
                log.Printf("Error marking event as processed: %v", err)
                d.Nack(false, true)
                continue
            }

            // Commit transaction
            if err := tx.Commit(); err != nil {
                log.Printf("Error committing transaction: %v", err)
                d.Nack(false, true)
                continue
            }

            log.Printf("Successfully processed order ID: %d from event: %s", orderID, event.EventID)
            d.Ack(false)
        }
    }()

    <-forever
}