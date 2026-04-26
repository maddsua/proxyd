# Instance configuration

A config location can be by using --config flag. By default proxyd tries to load it from `/etc/proxyd/proxyd.yml`, or from the current working directory.

```yml
debug: true|false # enables debug logging

manager: # proxy server configuration
  type: static|rpc|radius # which type of proxy manager to use

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

  # ---- RADIUS ONLY ----

  radius_auth_addr: 127.0.0.1:1812 # primary radius auth server addr
  radius_acct_addr: 127.0.0.1:1813 # optional accounting address. if left empty, the primary auth addr will be used for accounting as well
  dac_listen_addr: 127.0.0.1:3799 # this instance's DAC listen address
  radius_secret: testsecret # shared radius secret
  services: # list of services to spin-up
    - bind_addr: 127.0.0.1:1080
      type: socks
    - bind_addr: 127.0.0.1:8080
      type: http
      http_forward_enabled: true

  # ----

rpc_server: # rpc server configuration
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
radius_server:
  listen_addr: 127.0.0.1:1812 # server's own address. the same address must be passed to the radius manager config of the instance that acts as the proxy orchestrator
  dac_addr: 127.0.0.1:3799 # listen address of the proxy instance's DAC
  secret: testsecret # shared radius secret
  users: # user list, defined in the same way as for static config
    - username: maddsua
      password: testpass
```
