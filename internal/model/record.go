package model

type Record struct {
	Name           string   `json:"name"`
	FQDN           string   `json:"fqdn"`
	Type           string   `json:"type"`
	Scope          string   `json:"scope"`
	Owner          string   `json:"owner"`
	SourceOfTruth  string   `json:"source_of_truth"`
	Targets        []string `json:"targets"`
	Status         string   `json:"status"`
	DesiredValues  []string `json:"desired_values,omitempty"`
	ObservedValues []string `json:"observed_values,omitempty"`
	DesiredTTL     string   `json:"desired_ttl,omitempty"`
	ObservedTTL    string   `json:"observed_ttl,omitempty"`
}
