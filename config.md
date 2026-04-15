# Instance configuration

A config location can be by using --config flag. By default proxyd tries to load it from `/etc/proxyd/proxyd.yml`, or from the current working directory.

```yml
debug: true|false # enables debug logging

manager: # proxy server configuration
  type: static|rpc # which type of proxy manager to use

  # ---- STATIC ONLY ----
  services:
    - bind_addr: <ip:port> # proxy service bind address
	  type: socks|http # proxy service type
	  users: # user list, obviously
        - username: <myuser>
		  password: <super secret password>
		  suspended: true|false # allows to disable a user without completely removing their credentials
		  max_conn: <int> # sets a total limit for concurrent tcp connections
		  bandwidth_kbit: <int> # sets bandwidth limit in kbit/s
		  dns: <ip:port> # dns server to use for this peer
		  outbound_addr: <ip> # local address to use during dials
  # ----

  # ---- RPC ONLY ----
  endpoint_url: <http url> # point to your auth server
  secret_token: <token string> # your secret token formatted as one signle string

  # ----

rpc: # rpc server configuration
  listen_addr: <ip:port> # does this need an explaination?
  instances: # list of managers to accept
    - id: <uuid> # instance UUID
      secret: <url-base64-encoded secret>
      services:
      - bind_addr: <ip:port> # proxy service bind address
        type: socks|http # proxy service type
        users: # user list, obviously
          - username: <myuser>
            password: <super secret password>
            suspended: true|false # allows to disable a user without completely removing their credentials
            max_conn: <int> # sets a total limit for concurrent tcp connections
            bandwidth_kbit: <int> # sets bandwidth limit in kbit/s
            dns: <ip:port> # dns server to use for this peer
            outbound_addr: <ip> # local address to use during dials
```
