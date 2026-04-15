# RPC

The purpose of the RPC thingy is to allow you to control a proxy service remotely without relying too heavily on existing protocols such as RADIUS.

It works by periodically calling up the server and asking what the `proxytable` looks like and subsequantly updating its state.

## proxytable

Practically speaking, `proxytable` is a full instance configuration including service and peer settions. Think of it as of iptables but for proxies

Method handlers are regualr REST handlers, usually residing on a `/proxyd/rpc/v1` prefix.

Every request is authorized with an instance token via the `Authorization` header: `Authorization: Bearer <token>`.

Responses are wrapped into a Result object:

```typescript
type Result<T> = {
  data: T | null
  error: RPCError | null
}
```

To signal an error to the client type `RPCError` object is used:

```typescript
type RPCError = {
  message: string
  cause?: string
}
```

If a procedure doesn't need to return neither data nor an error an empty a `204 No Content` response must be used instead,

## Methods

The RPC defines the following methods:

### `GET /ready`

Used to verfiy that the auth server URL is valid and properly-ish configured. An instance will try to call this method before starting its proxy manager.

A server must return a `204 No Content` response if the auth token provided by the instance is valid, and with a JSON error object otherwise.

### `POST /status`

As soon as a proxy instance is up it will call this method to notify that it's ready. After that, it will periodically call it again to provide uptime updates.

Request payload:

```typescript
type InstanceStatus = {
  run_id: string // unique instance run uuid
  uptime: number // uptime in milliseconds
  services: ServiceStatus[]
}

type ServiceStatus = {
  bind_addr: string // network address such as 127.0.0.1:1080
  type: 'http' | 'socks'
  up: boolean 
  peers: PeerStatus[]
  error?: string
}

type PeerStatus = {
  id: string
  username?: string
  enabled: boolean
}
```

A server must respond either with a `204 No Content` or with a JSON error object.

### `POST /traffic`

Reports how much data the peers have transferred.

Request payload:

```typescript
type InstanceTrafficUpdate = {
  deltas: TrafficDelta[]
}

type TrafficDelta = {
  peer_id: string
  // traffic volume in bytes
  rx: number
  tx: number
}
```

A server must respond either with a `204 No Content` or with a JSON error object.

### `GET /proxytable`

An instance calls this method to request its current configuration.

There is no request payload.

A server must respond with a `Result<ProxyTable>` JSON object.

Response payload:

```typescript
type ProxyTable = {
  services: ProxyServiceEntry[]
}

type ProxyServiceEntry = {
  bind_addr: string
  service: 'http' | 'socks'
  http_forward_enabled?: boolean
  peers: ProxyTablePeerEntry[]
}

type ProxyTablePeerEntry = {
  id: string
  userinfo?: ProxyPeerUserInfo
  enabled: boolean
  max_connections?: number
  bandwidth?: ProxyPeerBandwidth
  dns?: string // dns server address
  outbound_addr?: string // an ip address to use in dials
}

type ProxyPeerUserInfo = {
  username: string
  password: string
  options?: PeerLoginOptions
}

type PeerLoginOptions = {
  // sets max number of failed attempts after which even valid user credentials won't be accepted
  max_attempts: number
  // sets reset period in seconds after which a user can try to log in again
  attempt_window: number
}

type ProxyPeerBandwidth = {
  // max peer connection speed in bytes/second, NOT BITS!
  rx: number
  tx: number
}
```
