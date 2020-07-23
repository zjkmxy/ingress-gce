#! /bin/bash
sudo gsutil cp gs://xinyu-vm-test/run_vm.sh /root/run_vm.sh
sudo chmod a+x /root/run_vm.sh
sudo /root/run_vm.sh startup

sudo crontab -l > ./cron.txt
echo "*/1 * * * * sudo /root/run_vm.sh update" >> ./cron.txt
crontab ./cron.txt
rm ./cron.txt

