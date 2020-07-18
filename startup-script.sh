#! /bin/bash
sudo apt-get update -y
sudo apt-get install curl -y

export TOKEN=<token-decoded>
export SERVERCA=<ca.pem-in-base64>
export SERVERIP=<server-ip>
export CLUSTERNAME=<cluster-name>
export ACCOUNTNAME=vm

sudo mkdir -p ~/.kube
echo 'apiVersion: v1
kind: Config
users:
- name: '$ACCOUNTNAME'
  user:
    token: '$TOKEN'
clusters:
- cluster:
    certificate-authority-data: '$SERVERCA'
    server: https://'$SERVERIP'
  name: '$CLUSTERNAME'
contexts:
- context:
    cluster: '$CLUSTERNAME'
    user: '$ACCOUNTNAME'
  name: '$CLUSTERNAME'
current-context: '$CLUSTERNAME | sudo tee ~/.kube/config
curl -LO https://storage.googleapis.com/kubernetes-release/release/`curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt`/bin/linux/amd64/kubectl
chmod a+x ./kubectl
sudo mv ./kubectl /usr/local/bin/kubectl

curl -H "Metadata-Flavor: Google" http://metadata/computeMetadata/v1/instance/network-interfaces/0/ip | tee ./addr.txt
echo 'apiVersion: "cloud.google.com/v1alpha1"                                                        
kind: VMInstance
metadata:
  name: '`/bin/hostname`'
  labels:
    tier: vm
spec:
  hostName: '`/bin/hostname`'
  ip: '`cat ./addr.txt` | tee ./cr.yaml
sudo /usr/local/bin/kubectl apply -f ./cr.yaml

