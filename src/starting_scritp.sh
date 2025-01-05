# this is not really a script, but a collection of commands to run in the terminal
# make sure:
# docker and minikube is installed
# rust, go and build essentials migth be also needed
# auto completez for docker, kubectl, helm...


# elo lepesek
## ha meg nincs megfelelo mappa
mkdir -p ~/pvc
chmod 777 ~/pvc
## acr
az login
az group create --name skala_nhf_ad0mka --location northeurope
az acr create --resource-group skala_nhf_ad0mka               --name acrad0mka               --sku Basic               --admin-enabled true
### rakd be github secretbe!!!
az acr credential show --name acrad0mka





## start minikube, rakd bele az ip-d
minikube start --listen-address=0.0.0.0 --apiserver-ips={} --mount-string="/home/ubi/pvc:/mnt/data" --mount
## kulso elereshez kell sajnos mivel csak vmben tudtad futtatni a minikubeot, viszont a virtualizacio miatt konnyeden csak a dockeres driver ment, igy fontos a socat parancsot is kiadni, kulonben nem lesz elerheto github action altal!
socat TCP-LISTEN:8443,fork,bind=0.0.0.0,reuseaddr TCP:$(minikube ip):8443
## kubeconfig secretbe!
kubectl config view --minify --raw --flatten

## arcba kotes 
### https://learn.microsoft.com/en-gb/azure/azure-arc/kubernetes/quickstart-connect-cluster?tabs=azure-cli#register-providers-for-azure-arc-enabled-kubernetes
az extension add --name connectedk8s
az provider register --namespace Microsoft.Kubernetes
az provider register --namespace Microsoft.KubernetesConfiguration
az provider register --namespace Microsoft.ExtendedLocation
az group create --name skala_nhf_ad0mka --location northeurope
az connectedk8s connect --name ArcTry --resource-group skala_nhf_ad0mka
AAD_ENTITY_ID=$(az ad signed-in-user show --query userPrincipalName -o tsv)
kubectl create clusterrolebinding demo-user-binding --clusterrole cluster-admin --user=$AAD_ENTITY_ID
### torlesre
az connectedk8s delete --name ArcTry --resource-group skala_nhf_ad0mka

# testeleshez lokalban
## elotte allitsad at values.yamlben az image pull policyt es a tag-et
eval $(minikube docker-env)
docker build --no-cache -t restaurant/menu:latest ./menu

docker build --no-cache -t restaurant/waiter:latest ./waiter
docker build --no-cache -t restaurant/chef:latest ./chef
helm install restaurant ./restaurant





# Test

minikube tunnel

MENU_IP=$(kubectl get svc menu -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
WAITER_IP=$(kubectl get svc waiter -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
RABBITMQ_IP=$(kubectl get svc rabbitmq -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Get ports (since you have fixed ports in your services, you could hardcode these)
MENU=$MENU_IP:$(kubectl get svc menu -o jsonpath='{.spec.ports[0].port}')
WAITER=$WAITER_IP:$(kubectl get svc waiter -o jsonpath='{.spec.ports[0].port}')

RABBITMQ=$RABBITMQ_IP:$(kubectl get svc rabbitmq -o jsonpath='{.spec.ports[1].port}')  

echo "MENU $MENU"
echo "WAITER $WAITER"
echo "RABBITMQ in browser $RABBITMQ"

curl http://$MENU/menu
echo ""
curl -X POST http://$MENU/menu \
-H "Content-Type: application/json" \
-d '{
  "name": "Gulyásleves",
  "price": 1500,
  "is_available": true
}'
echo ""
curl -X POST http://$MENU/menu \
-H "Content-Type: application/json" \
-d '{
  "name": "kóla",
  "price": 500,
  "is_available": true
}'
echo ""
curl -X POST http://$MENU/menu \
-H "Content-Type: application/json" \
-d '{
  "name": "Lángos",
  "price": 500,
  "is_available": true
}'
echo ""
curl -X POST http://$MENU/menu \
-H "Content-Type: application/json" \
-d '{
  "name": "Dobos Torta",
  "price": 1000,
  "is_available": true
}'

echo ""
curl http://$MENU/menu
echo ""


curl -X POST http://$MENU/menu \
-H "Content-Type: application/json" \
-d '{
  "name": "Hortobágyi Palacsinta",
  "price": 1000,
  "is_available": true
}'
echo ""
curl http://$MENU/menu
echo ""

curl -X POST http://$WAITER/order \
-H "Content-Type: application/json" \
-d '{
  "table_number": 1,
  "items": [
    {
      "menu_item_id": 1,
      "quantity": 2
    },
    {
      "menu_item_id": 2,
      "quantity": 1
    }
  ]
}'
echo ""
curl -X POST http://$WAITER/order \
-H "Content-Type: application/json" \
-d '{
  "table_number": 2,
  "items": [
    {
      "menu_item_id": 3,
      "quantity": 1
    },
    {
      "menu_item_id": 4,
      "quantity": 2
    }
  ]
}'
echo ""
curl -X POST http://$WAITER/order \
-H "Content-Type: application/json" \
-d '{
  "table_number": 3,
  "items": [
    {
      "menu_item_id": 1,
      "quantity": 1
    },
    {
      "menu_item_id": 2,
      "quantity": 1
    },
    {
      "menu_item_id": 3,
      "quantity": 1
    },
    {
      "menu_item_id": 4,
      "quantity": 1
    }
  ]
}'
echo ""
curl -X POST http://$WAITER/order \
-H "Content-Type: application/json" \
-d '{
  "table_number": 4,
  "items": [
    {
      "menu_item_id": 2,
      "quantity": 3
    },
    {
      "menu_item_id": 4,
      "quantity": 1
    }
  ]
}'
echo ""


### itt varni kell

curl http://$WAITER/orders/1
echo ""
curl  -X POST  http://$WAITER/orders/1/pay
echo ""
curl http://$WAITER/orders/1


curl  -X POST  http://$WAITER/orders/2/pay
curl  -X POST   http://$WAITER/orders/3/pay
curl -X POST   http://$WAITER/orders/4/pay



# For debugging locally
kubectl exec -it $(kubectl get pods -l app=postgres -o jsonpath="{.items[0].metadata.name}") -- psql -U restaurant -d restaurant -c "SELECT * FROM menu_items;"
kubectl exec -it $(kubectl get pods -l app=postgres -o jsonpath="{.items[0].metadata.name}") -- psql -U restaurant -d restaurant -c "SELECT * FROM completed_orders;"
kubectl exec -it $(kubectl get pods -l app=postgres -o jsonpath="{.items[0].metadata.name}") -- psql -U restaurant -d restaurant -c "\dt"

# jelentes keszito job
kubectl delete job orders-report
kubectl apply -f /home/ubi/skala_nhf/src/orders-report-job.yaml
sleep 2s
kubectl logs $(kubectl get pods --selector=job-name=orders-report --output=jsonpath='{.items[0].metadata.name}')