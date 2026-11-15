package discovery

import (
	"context"
	"net"
)

const (
	DefaultServiceType = "_ai-gateway._tcp"

	DefaultDomain = "local."

	SubtypeChatCompletions = "_chat-completions"

	SubtypeResponses = "_responses"

	SubtypeMessages = "_messages"

	SubtypeGenerateContent = "_generate-content"

	SubtypeInteractions = "_interactions"

	SubtypeCompletions = "_completions"

	SubtypeResponsesWS = "_responses-ws"

	SubtypeRealtime = "_realtime"

	SubtypeImages = "_images"

	SubtypeVideos = "_videos"

	SubtypeAudio = "_audio"

	SubtypeCountTokens = "_count-tokens"

	SubtypeSearch = "_search"

	ProductCPA = "cliproxyapi"
)

type ServiceSpec struct {
	InstanceName  string
	ServiceType   string
	Domain        string
	Port          int
	Subtypes      []string
	TextRecords   []string
	Interfaces    []net.Interface
	AdvertisedIPs []string
}

type DiscoveredService struct {
	InstanceName string   `json:"instance_name"`
	ServiceType  string   `json:"service_type"`
	Domain       string   `json:"domain"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	IPv4         []net.IP `json:"ipv4"`
	IPv6         []net.IP `json:"ipv6"`

	InterfaceIndex int `json:"interface_index,omitempty"`

	IPv6Zones map[string]string `json:"ipv6_zones,omitempty"`

	Path         string   `json:"path,omitempty"`
	TLS          bool     `json:"tls"`
	Protocols    []string `json:"protocols,omitempty"`
	Product      string   `json:"product,omitempty"`
	InstanceID   string   `json:"instance_id,omitempty"`
	AuthRequired bool     `json:"auth_required"`
	AuthMethods  []string `json:"auth_methods,omitempty"`

	Endpoints map[string]string `json:"endpoints,omitempty"`
	NodeRole  string            `json:"node_role,omitempty"`

	Version string            `json:"version,omitempty"`
	RawTXT  map[string]string `json:"raw_txt,omitempty"`
}

type Advertiser interface {
	Start(ctx context.Context, spec ServiceSpec) error
	Stop() error
}

type Browser interface {
	Browse(ctx context.Context, serviceType, domain string) ([]DiscoveredService, error)
	BrowseWithFallback(ctx context.Context) ([]DiscoveredService, error)
	BrowseWithFallbackServiceType(ctx context.Context, serviceType string) ([]DiscoveredService, error)
}
