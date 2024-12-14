// waiter/main.go
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
	// Connect to RabbitMQ
	amqpAddr := os.Getenv("AMQP_ADDR")
	if amqpAddr == "" {
		amqpAddr = "amqp://guest:guest@10.0.2.15:5672/"
	}

	var err error
	amqpConn, err = amqp.Dial(amqpAddr)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer amqpConn.Close()

	amqpChannel, err = amqpConn.Channel()
	if err != nil {
		log.Fatalf("Failed to open channel: %v", err)
	}
	defer amqpChannel.Close()

	_, err = amqpChannel.QueueDeclare(
		"orders",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to declare queue: %v", err)
	}

	r := mux.NewRouter()
	r.HandleFunc("/order", handleOrder).Methods("POST")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	log.Printf("Server starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func handleOrder(w http.ResponseWriter, r *http.Request) {
	var order Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	orderJSON, err := json.Marshal(order)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = amqpChannel.PublishWithContext(ctx,
		"",
		"orders",
		false, // mandatory!
		false, // immediate!
		amqp.Publishing{
			ContentType: "application/json",
			Body:        orderJSON,
		})
	if err != nil {
		http.Error(w, "Failed to publish order", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(order)
}
