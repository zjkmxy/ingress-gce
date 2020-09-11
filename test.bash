echo "execute 'curl http://hostname.default.svc.cluster.local/' ..."
kubectl exec -it client-pod-d999f4ff5-2vpsn -c client-pod -- bash -c " while true; do curl http://hostname.default.svc.cluster.local/; echo; sleep 1; done"

