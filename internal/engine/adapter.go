package engine

type Adapter interface {
	Ban(ip string, isParmanent bool, durationSeconds int) (string, error)
	UnBan(ip string) (string, error)
}

func NewAdapter(name, gatewayHost string) Adapter {
	switch name {
	case "xdp":
		return &XDPAdapter{GatewayHost: gatewayHost}
	case "iptables":
		return &IptablesAdapter{GatewayHost: gatewayHost}
	default:

		return &XDPAdapter{GatewayHost: gatewayHost}
	}
}
