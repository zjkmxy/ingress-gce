#!/bin/bash

. $(dirname ${BASH_SOURCE})/util.sh

desc "Bootstrap: configure RBAC"
run "nvim ./docs/experimental/workload/rbac.yaml"

desc "Bootstrap: deploy the controller"
run "cat ./docs/experimental/workload/controller-deploy.yaml"
run "kubectl get pod -o wide"

desc "Create the service"
run "kubectl get svc"
run "kubectl describe svc hostname"
run "kubectl get endpointslices"

desc "Before the workload starts"
run "kubectl get wl"

desc "Start the workload ..."
run "kubectl get wl -w"
run "kubectl describe wl"
run "kubectl get endpointslices"

desc "Shutdown the workload ..."
run "watch -n 2 kubectl get wl"
run "kubectl get endpointslices"

