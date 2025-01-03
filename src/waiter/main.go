package main

import (
    "context"
    "encoding/json"
    "log"
    "net/http"
    "os"
    "time"
    "database/sql"

    "github.com/gorilla/mux"
    amqp "github.com/rabbitmq/amqp091-go"
    "github.com/google/uuid"
    _ "github.com/lib/pq"
)

type OrderItem struct {
    MenuItemID int `json:"menu_item_id"`
    Quantity   int `json:"quantity"`
}

type Order struct {
    TableNumber int         `json:"table_number"`
    Items       []OrderItem `json:"items"`
    Subtotal  int     `json:"subtotal"`
}

type OrderEvent struct {
    EventID   string    `json:"event_id"`
    EventType string    `json:"event_type"`
    Timestamp time.Time `json:"timestamp"`
    Order     Order `json:"order"`
}

var amqpConn *amqp.Connection
var amqpChannel *amqp.Channel

const version = "1.0.2"

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
    r.HandleFunc("/orders/{tableNumber}", getOrders).Methods("GET")
    r.HandleFunc("/orders/{tableNumber}/pay", markOrdersAsPaid).Methods("POST")
    r.HandleFunc("/version", getVersion).Methods("GET")

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

    log.Printf("Decoded order: Table %d, %d items",
        order.TableNumber,
        len(order.Items))
    
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

    totalPrice := 0
    for _, item := range order.Items {
        var price int
        err := db.QueryRow("SELECT price FROM menu_items WHERE id = $1", item.MenuItemID).Scan(&price)
        if err != nil {
            log.Printf("Error querying price for item %d: %v", item.MenuItemID, err)
            http.Error(w, "Internal server error", http.StatusInternalServerError)
            return
        }
        totalPrice += price * int(item.Quantity)
    }

    order.Subtotal = int(totalPrice)
    event := OrderEvent{
        EventID:   uuid.New().String(),
        EventType: "OrderCreated",
        Timestamp: time.Now(),
        Order:     order,
    }

    eventJSON, err := json.Marshal(event)
    if err != nil {
        log.Printf("Error marshalling order event: %v", err)
        http.Error(w, "Failed to process order", http.StatusInternalServerError)
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
            Body:        eventJSON,
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
    log.Printf("Order processing complete, response sent to chef")
}

func getOrders(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    tableNumber := vars["tableNumber"]

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

    rows, err := db.Query("SELECT table_number, items, subtotal FROM completed_orders WHERE table_number = $1 AND paid = false", tableNumber)
    if err != nil {
        log.Printf("Error querying orders: %v", err)
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }
    defer rows.Close()

    var orders []Order
    for rows.Next() {
        var order Order
        var items string
        if err := rows.Scan(&order.TableNumber, &items, &order.Subtotal); err != nil {
            log.Printf("Error scanning order: %v", err)
            http.Error(w, "Internal server error", http.StatusInternalServerError)
            return
        }
        if err := json.Unmarshal([]byte(items), &order.Items); err != nil {
            log.Printf("Error unmarshalling items: %v", err)
            http.Error(w, "Internal server error", http.StatusInternalServerError)
            return
        }
        orders = append(orders, order)
    }

    w.Header().Set("Content-Type", "application/json")
    if err := json.NewEncoder(w).Encode(orders); err != nil {
        log.Printf("Error encoding response: %v", err)
        http.Error(w, "Internal server error", http.StatusInternalServerError)
    }
}

func markOrdersAsPaid(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    tableNumber := vars["tableNumber"]

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

    _, err = db.Exec("UPDATE completed_orders SET paid = true WHERE table_number = $1 AND paid = false", tableNumber)
    if err != nil {
        log.Printf("Error updating orders: %v", err)
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
}

func getVersion(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"version": version})
}