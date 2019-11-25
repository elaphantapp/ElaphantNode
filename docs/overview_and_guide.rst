System Requirements and Installation Guide
########################################

.. toctree::
  :maxdepth: 2

Installation
============
Following is how we build elephant node in ubuntu.

Ubuntu
******

Install essentials and Go ::

    $ sudo apt-get install -y software-properties-common
    $ sudo add-apt-repository -y ppa:gophers/archive
    $ sudo apt update
    $ sudo apt-get install -y golang-1.12-go

Setup basic workspace,In this instruction we use ~/dev/src/github.com/elastos as our working directory. If you clone the source code to a different directory, please make sure you change other environment variables accordingly (not recommended)::

    $ mkdir -p ~/dev/bin
    $ mkdir -p ~/dev/src/github.com/elastos/

Set correct environment variables::

    export GOROOT=/usr/local/opt/go@1.12/libexec
    export GOPATH=$HOME/dev
    export GO111MODULE=on
    export GOBIN=$GOPATH/bin
    export PATH=$GOROOT/bin:$PATH
    export PATH=$GOBIN:$PATH

Clone source code to $GOPATH/src/github/elastos folder,Make sure you are in the folder of $GOPATH/src/github.com/elastos::

    $ git clone https://github.com/elastos/Elastos.ELA.Elephant.Node.git

If clone works successfully, you should see folder structure like $GOPATH/src/github.com/elastos/Elastos.ELA.Elephant.Node/Makefile

Make,Build the node::

    $ cd $GOPATH/src/github.com/elastos/Elastos.ELA.Elephant.Node
    $ make

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


If you did not see any error message, congratulations, you have made the Elephant full node.

Run the node::

    $ sudo apt-get install build-essential
    $ make
    $ ./elaphant

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