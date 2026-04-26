package radiuspkg

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"time"

	radius "github.com/maddsua/layeh-radius"
	"github.com/maddsua/layeh-radius/rfc2865"
	"github.com/maddsua/layeh-radius/rfc2866"
	"github.com/maddsua/layeh-radius/rfc3162"
	"github.com/maddsua/layeh-radius/rfc4372"
	"github.com/maddsua/layeh-radius/rfc4679"
	"github.com/maddsua/layeh-radius/rfc6911"
	"github.com/maddsua/proxyd/utils"
)

type PeerCredentials struct {
	Username  string
	Password  string
	ProxyHost net.Addr
	UserAddr  net.Addr
}

func (params *PeerCredentials) Hash() string {

	hasher := sha1.New()

	hasher.Write([]byte(params.Username))
	hasher.Write([]byte(params.Password))

	if addr := params.ProxyHost; addr != nil {
		hasher.Write([]byte(addr.String()))
	}

	if addr := params.UserAddr; addr != nil {
		hasher.Write([]byte(addr.String()))
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

func (params *PeerCredentials) MarshalPacket(packet *radius.Packet) error {

	if err := rfc2865.ServiceType_Set(packet, rfc2865.ServiceType_Value_FramedUser); err != nil {
		return fmt.Errorf("rfc2865.ServiceType_Set: %v", err)
	}

	if err := rfc2865.UserName_SetString(packet, params.Username); err != nil {
		return fmt.Errorf("rfc2865.UserName_SetString: %v", err)
	}

	if err := rfc2865.UserPassword_SetString(packet, params.Password); err != nil {
		return fmt.Errorf("rfc2865.UserPassword_SetString: %v", err)
	}

	if addr, _ := utils.SplitIPPort(params.UserAddr); addr != nil {
		if err := rfc2865.CallingStationID_SetString(packet, addr.String()); err != nil {
			return fmt.Errorf("rfc2865.CallingStationID_SetString: %v", err)
		}
	}

	if addr, port := utils.SplitIPPort(params.ProxyHost); addr != nil {

		if ip := addr.To4(); ip != nil {
			if err := rfc2865.NASIPAddress_Set(packet, ip); err != nil {
				return fmt.Errorf("rfc2865.NASIPAddress_Set: %v", err)
			}
		}

		if ip := addr.To16(); ip != nil {
			if err := rfc3162.NASIPv6Address_Set(packet, ip); err != nil {
				return fmt.Errorf("rfc3162.NASIPv6Address_Set: %v", err)
			}
		}

		if err := rfc2865.NASPort_Set(packet, rfc2865.NASPort(port)); err != nil {
			return fmt.Errorf("rfc2865.NASPort_Set: %v", err)
		}
	}

	return nil
}

func ParsePeerCredentials(packet *radius.Packet) *PeerCredentials {

	params := PeerCredentials{
		Username: rfc2865.UserName_GetString(packet),
		Password: rfc2865.UserPassword_GetString(packet),
	}

	hostIP := rfc2865.NASIPAddress_Get(packet)
	if hostIP == nil {
		if hostIP = rfc3162.NASIPv6Address_Get(packet); hostIP == nil {
			hostIP = net.IPv4(0, 0, 0, 0)
		}
	}

	if hostPort := rfc2865.NASPort_Get(packet); hostPort > 0 {
		params.ProxyHost = &net.TCPAddr{IP: hostIP, Port: int(hostPort)}
	} else {
		params.ProxyHost = &net.IPAddr{IP: hostIP}
	}

	if ip := net.ParseIP(rfc2865.CallingStationID_GetString(packet)); ip != nil {
		params.UserAddr = &net.IPAddr{IP: ip}
	}

	return &params
}

type PeerAuthorization struct {
	ChargeableUserID string
	AcctSessionID    string
	FramedIP         net.IP
	DNSServer        net.IP
	Timeout          time.Duration
	IdleTimeout      time.Duration
	MaxRxRate        int64
	MaxTxRate        int64
	ConnectionLimit  int
}

func (peer *PeerAuthorization) MarshalPacket(packet *radius.Packet) error {

	if uid := peer.ChargeableUserID; uid != "" {
		if err := rfc4372.ChargeableUserIdentity_SetString(packet, uid); err != nil {
			return fmt.Errorf("rfc4372.ChargeableUserIdentity_SetString: %v", err)
		}
	}

	if sid := peer.AcctSessionID; sid != "" {
		if err := rfc2866.AcctSessionID_SetString(packet, sid); err != nil {
			return fmt.Errorf("rfc2866.AcctSessionID_SetString: %v", err)
		}
	}

	if ip := peer.FramedIP.To4(); ip != nil {
		if err := rfc2865.FramedIPAddress_Set(packet, ip); err != nil {
			return fmt.Errorf("rfc2865.FramedIPAddress_Set: %v", err)
		}
	} else if ip6 := peer.FramedIP.To16(); ip6 != nil {
		if err := rfc6911.FramedIPv6Address_Set(packet, ip6); err != nil {
			return fmt.Errorf("rfc6911.FramedIPv6Address_Set: %v", err)
		}
	}

	if ttl := int(peer.Timeout.Seconds()); ttl > 0 {
		if err := rfc2865.SessionTimeout_Set(packet, rfc2865.SessionTimeout(ttl)); err != nil {
			return fmt.Errorf("rfc2865.SessionTimeout_Set: %v", err)
		}
	}

	if idleTtl := int(peer.IdleTimeout.Seconds()); idleTtl > 0 {
		if err := rfc2865.IdleTimeout_Set(packet, rfc2865.IdleTimeout(idleTtl)); err != nil {
			return fmt.Errorf("rfc2865.IdleTimeout_Set: %v", err)
		}
	}

	if rx := peer.MaxRxRate; rx > 0 {
		if err := rfc4679.MaximumDataRateDownstream_Set(packet, rfc4679.MaximumDataRateDownstream(rx)); err != nil {
			return fmt.Errorf("rfc4679.MaximumDataRateDownstream_Set: %v", err)
		}
	}

	if tx := peer.MaxTxRate; tx > 0 {
		if err := rfc4679.MaximumDataRateUpstream_Set(packet, rfc4679.MaximumDataRateUpstream(tx)); err != nil {
			return fmt.Errorf("rfc4679.MaximumDataRateUpstream_Set: %v", err)
		}
	}

	if nconn := peer.ConnectionLimit; nconn > 0 {
		if err := rfc2865.PortLimit_Set(packet, rfc2865.PortLimit(nconn)); err != nil {
			return fmt.Errorf("rfc2865.PortLimit_Set: %v", err)
		}
	}

	if dns := peer.DNSServer.To16(); dns != nil {
		if err := rfc6911.DNSServerIPv6Address_Set(packet, dns); err != nil {
			return fmt.Errorf("rfc6911.DNSServerIPv6Address_Set: %v", err)
		}
	}

	return nil
}

func ParsePeerAuth(packet *radius.Packet) *PeerAuthorization {

	peer := PeerAuthorization{
		ChargeableUserID: rfc4372.ChargeableUserIdentity_GetString(packet),
		AcctSessionID:    rfc2866.AcctSessionID_GetString(packet),
		FramedIP:         rfc2865.FramedIPAddress_Get(packet),
		DNSServer:        rfc6911.DNSServerIPv6Address_Get(packet),
		Timeout:          time.Duration(rfc2865.SessionTimeout_Get(packet)) * time.Second,
		IdleTimeout:      time.Duration(rfc2865.IdleTimeout_Get(packet)) * time.Second,
		MaxRxRate:        int64(rfc4679.MaximumDataRateDownstream_Get(packet)),
		MaxTxRate:        int64(rfc4679.MaximumDataRateUpstream_Get(packet)),
		ConnectionLimit:  int(rfc2865.PortLimit_Get(packet)),
	}

	if peer.FramedIP == nil {
		peer.FramedIP = rfc6911.FramedIPv6Address_Get(packet)
	}

	return &peer
}

type AccountingDelta struct {
	Type             rfc2866.AcctStatusType
	SessionID        string
	ChargeableUserID string
	RxBytes          int64
	TxBytes          int64
}

func (params *AccountingDelta) IsZero() bool {
	return params.RxBytes <= 0 && params.TxBytes <= 0
}

func (params *AccountingDelta) MarshalPacket(packet *radius.Packet) error {

	if err := rfc2866.AcctStatusType_Set(packet, params.Type); err != nil {
		return fmt.Errorf("rfc2866.AcctStatusType_Set: %v", err)
	}

	if err := rfc2866.AcctSessionID_SetString(packet, params.SessionID); err != nil {
		return fmt.Errorf("rfc2866.AcctSessionID_Set: %v", err)
	}

	if params.ChargeableUserID != "" {
		if err := rfc4372.ChargeableUserIdentity_SetString(packet, params.ChargeableUserID); err != nil {
			return fmt.Errorf("rfc4372.ChargeableUserIdentity_SetString: %v", err)
		}
	}

	if params.RxBytes > 0 {
		if err := rfc2866.AcctInputOctets_Set(packet, rfc2866.AcctInputOctets(params.RxBytes)); err != nil {
			return fmt.Errorf("rfc2866.AcctInputOctets_Set: %v", err)
		}
	}

	if params.TxBytes > 0 {
		if err := rfc2866.AcctOutputOctets_Set(packet, rfc2866.AcctOutputOctets(params.TxBytes)); err != nil {
			return fmt.Errorf("rfc2866.AcctOutputOctets_Set: %v", err)
		}
	}

	return nil
}

func ParseAccountingDelta(packet *radius.Packet) *AccountingDelta {
	return &AccountingDelta{
		Type:             rfc2866.AcctStatusType_Get(packet),
		SessionID:        rfc2866.AcctSessionID_GetString(packet),
		ChargeableUserID: rfc4372.ChargeableUserIdentity_GetString(packet),
		RxBytes:          int64(rfc2866.AcctInputOctets_Get(packet)),
		TxBytes:          int64(rfc2866.AcctOutputOctets_Get(packet)),
	}
}
