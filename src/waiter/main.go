package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	amqp "github.com/rabbitmq/amqp091-go"
)

type OrderItem struct {
	MenuItemID int `json:"menu_item_id"`
	Quantity   int `json:"quantity"`
}

type Order struct {
	TableNumber int         `json:"table_number"`
	Items       []OrderItem `json:"items"`
	TotalPrice  float64     `json:"total_price"`
}

var amqpConn *amqp.Connection
var amqpChannel *amqp.Channel

func main() {
	log.Printf("Starting waiter service...")

	amqpAddr := os.Getenv("AMQP_ADDR")
	if amqpAddr == "" {
		amqpAddr = "amqp://guest:guest@rabbitmq:5672/"
		log.Printf("No AMQP_ADDR provided, using default: %s", amqpAddr)
	} else {
		log.Printf("Using configured AMQP_ADDR: %s", amqpAddr)
	}

	var err error
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		log.Printf("Attempting to connect to RabbitMQ (attempt %d/%d)...", i+1, maxRetries)
		amqpConn, err = amqp.Dial(amqpAddr)
		if err == nil {
			log.Printf("Successfully connected to RabbitMQ")
			break
		}
		log.Printf("Failed to connect to RabbitMQ: %v", err)
		if i < maxRetries-1 {
			log.Printf("Retrying in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}

	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ after %d attempts: %v", maxRetries, err)
	}
	defer amqpConn.Close()

	log.Printf("Opening RabbitMQ channel...")
	amqpChannel, err = amqpConn.Channel()
	if err != nil {
		log.Fatalf("Failed to open channel: %v", err)
	}
	defer amqpChannel.Close()

	log.Printf("Declaring orders queue...")
	_, err = amqpChannel.QueueDeclare(
		"orders", // queue name
		false,    // durable
		false,    // delete when unused
		false,    // exclusive
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare queue: %v", err)
	}
	log.Printf("Queue 'orders' declared successfully")

	r := mux.NewRouter()
	r.HandleFunc("/order", handleOrder).Methods("POST")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
		log.Printf("No PORT specified, using default: %s", port)
	}

	serverAddr := ":" + port
	log.Printf("Starting HTTP server on %s", serverAddr)
	log.Fatal(http.ListenAndServe(serverAddr, r))
}

func handleOrder(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received order request from %s", r.RemoteAddr)

	var order Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		log.Printf("Error decoding order: %v", err)
		http.Error(w, "Invalid order format", http.StatusBadRequest)
		return
	}

	log.Printf("Decoded order: Table %d, %d items, total price: %.2f",
		order.TableNumber,
		len(order.Items),
		order.TotalPrice)

	orderJSON, err := json.Marshal(order)
	if err != nil {
		log.Printf("Error marshaling order to JSON: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Printf("Publishing order to RabbitMQ queue 'orders'...")
	err = amqpChannel.PublishWithContext(ctx,
		"",       // exchange
		"orders", // routing key
		false,    // mandatory
		false,    // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        orderJSON,
		})

	if err != nil {
		log.Printf("Error publishing order to RabbitMQ: %v", err)
		http.Error(w, "Failed to process order", http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully published order to RabbitMQ")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(order); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
	log.Printf("Order processing complete, response sent to client")
}