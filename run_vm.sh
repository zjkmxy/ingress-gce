#! /bin/bash

readonly CLUSTERNAME=`curl -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/attributes/k8s-cluster-name"`
readonly CLUSTERZONE=`curl -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/attributes/k8s-cluster-zone"`
readonly HOSTNAME=`/bin/hostname`
readonly HOSTIP=`curl -H "Metadata-Flavor: Google" http://metadata/computeMetadata/v1/instance/network-interfaces/0/ip`
readonly ATTRIBUTES=`curl -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/attributes/"`

function startup() {
  curl -LO https://storage.googleapis.com/kubernetes-release/release/`curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt`/bin/linux/amd64/kubectl
  chmod a+x ./kubectl
  sudo mv ./kubectl /usr/local/bin/kubectl
  sudo gcloud container clusters get-credentials $CLUSTERNAME --zone $CLUSTERZONE
}

function create(){
  echo 'apiVersion: "cloud.google.com/v1alpha1"
kind: VMInstance
metadata:
  name: '$HOSTNAME'
  labels:' | tee ./cr.yaml
  for attr in $ATTRIBUTES
  do
      if [[ $attr = k8s-label-* ]]
      then
          echo "    "${attr:10}": "`curl -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/attributes/"$attr` >> ./cr.yaml
      fi
  done
  echo 'spec:
  hostName: '$HOSTNAME'
  ip: '$HOSTIP'
  heartBeat: '`date --rfc-3339=seconds --utc  | sed 's/ /T/; s/\+00:00/Z/'` >> ./cr.yaml

  sudo /usr/local/bin/kubectl apply -f ./cr.yaml
  rm ./cr.yaml
}

function heartbeat(){
  echo 'spec:
  heartBeat: '`date --rfc-3339=seconds --utc  | sed 's/ /T/; s/\+00:00/Z/'` | tee ./cr.yaml
  sudo /usr/local/bin/kubectl patch vm $HOSTNAME --patch "$(cat cr.yaml)" --type=merge
  rm ./cr.yaml
}

function shutdown(){
  sudo /usr/local/bin/kubectl delete vm `/bin/hostname`
}

case $1 in
  'startup')
    startup
    create
    ;;
  'update')
    heartbeat
    ;;
  'shutdown')
    shutdown
    ;;
  *)
    echo "Usage: $0 <startup|update|shutdown>" >&2
    ;;
esac

