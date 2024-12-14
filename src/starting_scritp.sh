minikube start
eval $(minikube docker-env)
docker build -t restaurant/menu:latest ./menu
docker build -t restaurant/waiter:latest ./waiter
docker compose up -d
helm install restaurant ./restaurant
kubectl get pods

(trap 'kill $(jobs -p); exit' INT; kubectl port-forward service/menu 8080:8080 & kubectl port-forward service/waiter 8081:8081 & wait)




# Test
# Add a menu item
curl -X POST http://localhost:8080/menu \
-H "Content-Type: application/json" \
-d '{
  "name": "Gulyásleves",
  "price": 2500,
  "isAvailable": true
}'

# Check menu items
curl http://localhost:8080/menu

# Submit an order
curl -X POST http://localhost:8081/order \
-H "Content-Type: application/json" \
-d '{
  "table_number": 1,
  "items": [
    {
      "menu_item_id": 1,
      "quantity": 2
    }
  ],
  "total_price": 5000
}'

# Check RabbitMQ management interface in browser
http://localhost:15672 (guest/guest)

# Check database for completed orders
docker exec -it src-postgres-1 psql -U restaurant -d restaurant -c "SELECT * FROM completed_orders;"