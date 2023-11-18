package ipx

import "github.com/mikioh/ipaddr"

type DefaultCIDRParser struct{}

func NewDefaultCIDRParser() *DefaultCIDRParser {
	return &DefaultCIDRParser{}
}

func (d DefaultCIDRParser) Parse(cidr string) (Range, error) {
	c, err := ipaddr.Parse(cidr)
	if err != nil {
		return Range{}, err
	}
	return Range{
		Start: c.First().IP,
		End:   c.Last().IP,
	}, nil
}
