# simple-mapreduce

## Install and Setup

Creation of the nodes

```
kubectl create -f reducer.yaml
kubectl create -f mapper.yaml
kubectl create -f master.yaml
```


Create Proxy
```
kubectl proxy --port=8080
```

Send Get Request
```
http://127.0.0.1:8080/api/v1/namespaces/default/pods/mapreduce-master/proxy/compute?text=Hello%20This%20is%20a%20Kubernetes%20Hello%20from%20a%20Go%20Map%20Kubernetes%20cluster
```
