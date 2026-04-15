# RADIUS attributes and notes

This document describes what attributes are used by proxyd over the RADIUS protocol.

## Access-Request

Request attributes:

| Attribute | Role | Required |
| --- | --- | --- |
| rfc2865.ServiceType | Informational | |
| rfc2865.UserName | Proxy user's name | + |
| rfc2865.UserPassword | Proxy user's password | + |
| rfc2865.CallingStationID | Original user's public IP address | |
| rfc2865.NASIPAddress | Proxy host's IP address, to which a user is connecting | |
| rfc3162.NASIPv6Address | Proxy host's IPv6 address, to which a user is connecting | |
| rfc2865.NASPort | Proxy host's port, to which a user is connecting | + |

Access-Accept attributes:

| Attribute | Role | Required |
| --- | --- | --- |
| rfc4372.ChargeableUserIdentity | Used as an optional PeerID to display in logs. If not present, username is used otherwise | |
| rfc2866.AcctSessionID | Traffic accounting session ID | |
| rfc2865.FramedIPAddress | Outbound IP address to give to the user | |
| rfc6911.FramedIPv6Address | Outbound IPv6 address to give to the user | |
| rfc2865.SessionTimeout | Time in seconds after a user must reauthenticated | |
| rfc2865.IdleTimeout | Time in seconds during which a users' session would be reauthenticated automatically, granted that some user activity was present | |
| rfc4679.MaximumDataRateDownstream | Max download speed in bits/s | |
| rfc4679.MaximumDataRateUpstream | Max upload speed in bits/s | |
| rfc2865.PortLimit | Concurrent connection limit | |
| rfc6911.DNSServerIPv6Address | DNS server IP address (doesn't have to actually be an IPv6) | |

Access-Reject attributes:

| Attribute | Role | Required |
| --- | --- | --- |
| rfc3576.ErrorCause | Optional rejection reason | |


## Accounting-Request

| Attribute | Role | Required |
| --- | --- | --- |
| rfc2866.AcctStatusType | RADIUS accounting type | + |
| rfc2866.AcctSessionID | RADIUS accounting session ID | + |
| rfc4372.ChargeableUserIdentity | Informational | |
| rfc2866.AcctInputOctets | Data downloaded since last report | |
| rfc2866.AcctOutputOctets | Data uploaded since last report | |

## Disconnect-Request

This is a DAC request, implying that the request is send backwards from the auth server to a proxy instance.

AcctSessionID is used as the session key and MUST be present.

Request attributes:

| Attribute | Role | Required |
| --- | --- | --- |
| rfc2866.AcctSessionID | Session ID to disconnect | + |

Response attributes:

| Attribute | Role | Required |
| --- | --- | --- |
| rfc3576.ErrorCause | Rejection reason | |

## Change-of-Authority-Request

This is a DAC request, implying that the request is send backwards from the auth server to a proxy instance.

AcctSessionID is used as the session key and MUST be present.

Request attributes:

| Attribute | Role | Required |
| --- | --- | --- |
| rfc4372.ChargeableUserIdentity | Used as an optional PeerID to display in logs. If not present, username is used otherwise | |
| rfc2866.AcctSessionID | Traffic accounting session ID | + |
| rfc2865.FramedIPAddress | Outbound IP address to give to the user | |
| rfc6911.FramedIPv6Address | Outbound IPv6 address to give to the user | |
| rfc2865.SessionTimeout | Time in seconds after a user must reauthenticated | |
| rfc2865.IdleTimeout | Time in seconds during which a users' session would be reauthenticated automatically, granted that some user activity was present | |
| rfc4679.MaximumDataRateDownstream | Max download speed in bits/s | |
| rfc4679.MaximumDataRateUpstream | Max upload speed in bits/s | |
| rfc2865.PortLimit | Concurrent connection limit | |
| rfc6911.DNSServerIPv6Address | DNS server IP address (doesn't have to actually be an IPv6) | |

Response attributes:

| Attribute | Role | Required |
| --- | --- | --- |
| rfc3576.ErrorCause | Rejection reason | |
