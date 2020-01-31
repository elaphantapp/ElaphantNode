System Requirements and Installation Guide
########################################

.. toctree::
  :maxdepth: 2

Installation
============
Following is how we build elephant node in ubuntu.

Ubuntu
******

Install elaphant for linux ::

    $ apt-get install build-essential
    $ adduser elaphant
    $ usermod -aG sudo elaphant
    $ su - elaphant
    $ sh -c "$(curl -fsSL https://raw.githubusercontent.com/Linuxbrew/install/master/install.sh)"
    $ echo 'eval $(/home/linuxbrew/.linuxbrew/bin/brew shellenv)' >>~/.profile
    $ source ~/.profile
    $ brew tap elaphantapp/elaphant
    $ brew install elaphant

update config.json similar to the following content::

    {
      "Configuration": {
        "ActiveNet": "mainnet",
        "HttpInfoPort": 20333,
        "HttpInfoStart": true,
        "HttpRestPort": 20334,
        "HttpRestStart": true,
        "HttpWsPort": 20335,
        "HttpWsStart":true,
        "HttpJsonPort": 20336,
        "EnableRPC": true,
        "NodePort": 20338,
        "PrintLevel": 1,
        "PowConfiguration": {
          "PayToAddr": "EeEkSiRMZqg5rd9a2yPaWnvdPcikFtsrjE",
          "MinerInfo": "ELA",
          "MinTxFee": 100
        },
        "RpcConfiguration": {
          "User": "clark",
          "Pass": "123456",
          "WhiteIPList": [
            "127.0.0.1"
          ]
        }
      }
    }

Extra feature configure::

    {
      // Whether or not earn node reward , if set to false , your node will not receive transaction reward
      "EarnReward":true,
      // How many utxo we bundle together to create multi-transaction
      "BundleUtxoSize":700
    }


if you only want to syncing the node to the current height, you can run::

    $ elaphant --pure

after fulling synced . you can stop the node and restart it with more startup options ::

    $ elaphant --key "{private key}" --password "{keystore password}"

Nginx config Example::

    server { # simple reverse-proxy
        server_name  exmaple.com;
        access_log   /var/log/nginx/node.access.log;

        # pass requests for dynamic content to rails/turbogears/zope, et al

        location ~ ^/api/[v]?1/wrap/rpc {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20336;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/node {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/block {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/transaction {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/asset {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/transactionpool {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }


        location ~ ^/api/[v]?1/balance {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/createTx {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/createVoteTx {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/history {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?2/history {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?3/history {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/sendRawTx {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/pubkey {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/currHeight {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/dpos {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/fee {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/node/reward/address {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/spend/utxos {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/tx {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/crc {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location ~ ^/api/[v]?1/simple {
            if ($request_method = OPTIONS ) {
              add_header "Access-Control-Allow-Origin"  *;
              add_header "Access-Control-Allow-Methods" "GET, POST, OPTIONS, HEAD";
              add_header "Access-Control-Allow-Headers" "Authorization, Origin, X-Requested-With, Content-Type, Accept";
              return 200;
            }
            proxy_pass http://localhost:20334;
            proxy_connect_timeout 120s;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }

        listen 443 ssl; # managed by Certbot
        ssl_certificate /etc/letsencrypt/live/exmaple.com/fullchain.pem; # managed by Certbot
        ssl_certificate_key /etc/letsencrypt/live/exmaple.com/privkey.pem; # managed by Certbot
        include /etc/letsencrypt/options-ssl-nginx.conf; # managed by Certbot
        ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem; # managed by Certbot

    }

    server {
        if ($host = exmaple.com) {
            return 301 https://$host$request_uri;
        } # managed by Certbot


        server_name  exmaple.com;
        listen 80;
        return 404; # managed by Certbot
    }