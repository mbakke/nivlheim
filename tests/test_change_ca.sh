#!/bin/bash

echo "-------------- Testing creating/activating a new client CA certificate -----------"
set -e

# Clean/init everything
sudo systemctl stop nivlheim
sudo rm -f /var/log/nivlheim/system.log /var/nivlheim/my.{crt,key} \
	/var/run/nivlheim_client_last_run /var/www/nivlheim/certs/* \
	/var/www/nivlheim/queue/*
echo -n | sudo tee /var/log/httpd/error_log
sudo -u apache /var/nivlheim/installdb.sh --wipe
sudo systemctl start nivlheim
sleep 4

# Run the client. This will call reqcert and post
if ! grep -s -e "^server" /etc/nivlheim/client.conf > /dev/null; then
    echo "server=localhost" | sudo tee -a /etc/nivlheim/client.conf
fi
curl -sS -X POST 'http://localhost:4040/api/v0/settings/ipranges' -d 'ipRange=127.0.0.0/24'
sudo /usr/sbin/nivlheim_client
if [[ ! -f /var/run/nivlheim_client_last_run ]]; then
    echo "The client failed to post data successfully."
    exit 1
fi

# Create a new CA certificate
sudo /usr/sbin/create_new_CA.sh

# Verify that the old client certificate still works
if ! sudo curl -sSkf --cert /var/nivlheim/my.crt --key /var/nivlheim/my.key \
	https://localhost/cgi-bin/secure/ping; then
	echo "The client cert didn't work after a new CA was created."
	exit 1
fi

# Ask for a new certificate, verify that they are still being signed with the old CA cert
A=`openssl x509 -in /var/nivlheim/my.crt -noout -issuer_hash`
sudo rm -f /var/nivlheim/my.* /var/run/nivlheim_client_last_run
sudo /usr/sbin/nivlheim_client
if [[ ! -f /var/run/nivlheim_client_last_run ]]; then
	echo "The client failed to run the second time."
	exit 1
fi
B=`openssl x509 -in /var/nivlheim/my.crt -noout -issuer_hash`
if [[ "$A" != "$B" ]]; then
	echo "After creating a new CA cert, it was used for issuing even before it was activated."
	exit 1
fi

# Activate the new CA certificate
sudo /usr/sbin/activate_new_CA.sh

# Verify that the old client certificate still works
if ! sudo curl -sSkf --cert /var/nivlheim/my.crt --key /var/nivlheim/my.key \
	https://localhost/cgi-bin/secure/ping; then
	echo "The client cert didn't work after a new CA was activated."
	exit 1
fi

# Ask for a new certificate, verify that it was signed with the new CA cert
sudo rm -f /var/nivlheim/my.* /var/run/nivlheim_client_last_run
sudo /usr/sbin/nivlheim_client
if [[ ! -f /var/run/nivlheim_client_last_run ]]; then
    echo "The client failed to run the third time."
    exit 1
fi
C=`openssl x509 -in /var/nivlheim/my.crt -noout -issuer_hash`
if [[ "$B" == "$C" ]]; then
    echo "Still signing with the old CA cert, even after the new one was activated."
    exit 1
fi

echo "Test result: OK"
