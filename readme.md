<img src="./proxyd-logo.svg" width="360px" />

# A pocket-sized proxy orchestration service

You know how annoying it is to rustle with config files, if you ever tried to update multiple proxy configurations at once. Even though some proxy services offer some sort of an administrative API, a lot of the time those are incomplete, undocumented, buggy or all of the above.

I am not claiming that this thing is any better, but making my own thing sounded like a better idea, rather than to spending probably even more time trying to make someone else's crappy code work the way I want.

Another aspect is strictly political. I don't really trust a bunch of commie vibe-coders when it comes to anything remotely related to security. And guess what - proxies are kinda mission critical, unless you use them to browse some adult websites. With the latest tendencies in the world however, I guess even if that's your entire threat model, you'd still want to be sure it ain't tracking your activity and beaming it straight to the party/ofcom/big_brotha/etc.

## Feature set

Anyway, as of now I don't got too much stuff going on here. For now it's just the basics.

### Proxy protocols

| Protocol | Status | Feature set | Auth methods | Note |
| --- | --- | --- | --- | --- |
| SOCKS 4 | ⚠️ Stub | - | Not supported | Is present for compatibility reasons to gracefully reject requests |
| SOCKS 5 | ⚠️ Partial support | TCP CONNECT, IPv6 | Usename/password | BIND not supported; UDP not supported |
| HTTP | ✅ Full support | FORWARD, CONNECT (TUNNEL), IPv6 | Basic auth | Full http/1.1 proxy spec support (allegedly, I didn't even read it thrugh) |

### Management interfaces

#### Static config

If you don't feel like enjoying dynamic user configuration, you could specify everything in a config file. This would defeat the purpose of this project entirely but hey, you have the option to do that anyway.

A sample config would look something like this:

```yml
manager:
  type: static
  services:
    - bind_addr: 172.217.19.174:1080
      type: socks
      users:
        - username: maddsua
          password: mysupersecretpass
          max_conn: 256
          bandwidth_kbit: 100
          dns: 1.1.1.1
          outbound_addr: 172.217.19.174
```

#### RADIUS

RADIUS is used to authenticate connecting users and manage their sessions. You'd have to define the list of running proxy services in advance.

Example config:

```yml
manager:
  type: radius
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
```

Refer to the [RADIUS section](./radius.md) to learn more about autorizing users and the attributes used.

#### proxytables

proxytables is a REST-based API that allows to configure literally everything here without relying on third-party tools or protocols.

It is described [here](./rpc_spec.md)

### Configuration

Default config location: `/etc/proxyd/proxyd.yml`.

Refer to the [full configuration example](./config.md) for more.

### Deployment target

It's best to deploy proxyd directly onto a VPS as it needs to see the original IP addresses of incoming connections.

#### Docker deploys

When deploying with Docker, the `host` networking mode is highly suggested, if not required.

Without it you won't be able to bind to specific IP addresses and the whole thing with managing ports will become a lot more complicated.

On other words, don't forget to add `--network host` before your `docker run` command.
