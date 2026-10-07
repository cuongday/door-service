package discoverymanager

import (
	"fmt"
	"net/netip"
)

func expandIPv4Range(from, to string) ([]string, error) {
	return expandIPv4RangeLimited(from, to, int(^uint(0)>>1))
}

func expandIPv4RangeLimited(from, to string, limit int) ([]string, error) {
	if limit < 1 {
		return nil, fmt.Errorf("IPv4 range limit must be positive")
	}
	start, err := netip.ParseAddr(from)
	if err != nil || !start.Is4() {
		return nil, fmt.Errorf("invalid IPv4 address %q", from)
	}
	if to == "" {
		to = from
	}
	end, err := netip.ParseAddr(to)
	if err != nil || !end.Is4() {
		return nil, fmt.Errorf("invalid IPv4 address %q", to)
	}
	if start.Compare(end) > 0 {
		return nil, fmt.Errorf("IPv4 range starts after it ends")
	}

	result := make([]string, 0)
	for current := start; ; current = current.Next() {
		if len(result) == limit {
			return nil, fmt.Errorf("IPv4 range exceeds target limit")
		}
		result = append(result, current.String())
		if current == end {
			break
		}
	}
	return result, nil
}
