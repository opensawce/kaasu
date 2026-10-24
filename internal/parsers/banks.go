package parsers

import "regexp"

// Sender ids carry an optional two-letter operator prefix and suffix, for example VM-HDFCBK-S.
func sender(core string) *regexp.Regexp {
	return regexp.MustCompile(`^([A-Z]{2}-?)?` + core + `(-[A-Z])?$`)
}

var hdfc = bank{
	name:    "HDFC",
	senders: []*regexp.Regexp{sender("HDFCBK")},
	domains: []string{"hdfcbank.net"},
	credit:  regexp.MustCompile(`Rs\.?(?P<amount>[0-9,]+\.[0-9]{1,2}) credited to a/c XX(?P<account>[0-9]{4}) on (?P<date>[0-9]{2}-[0-9]{2}-[0-9]{2}) by VPA (?P<payer>\S+) \(UPI Ref No (?P<utr>[0-9]{12})\b\)`),
	layout:  "02-01-06",
	extract: func(m map[string]string) (string, string, string) {
		return m["utr"], maskPayer(m["payer"]), m["account"]
	},
}

var sbi = bank{
	name:    "SBI",
	senders: []*regexp.Regexp{sender("SBIUPI"), sender("SBIINB"), sender("SBIPSG")},
	domains: []string{"sbi.co.in"},
	credit:  regexp.MustCompile(`a/c no\. XXXXXXXX(?P<account>[0-9]{4}) is credited by Rs\.?(?P<amount>[0-9,]+\.[0-9]{2}) on (?P<date>[0-9]{2}[A-Za-z]{3}[0-9]{2}) transfer from (?P<payer>.+?) Ref No (?P<utr>[0-9]{12})\b`),
	layout:  "02Jan06",
	extract: func(m map[string]string) (string, string, string) {
		return m["utr"], maskName(m["payer"]), m["account"]
	},
}

var icici = bank{
	name:    "ICICI",
	senders: []*regexp.Regexp{sender("ICICIB")},
	domains: []string{"icicibank.com"},
	credit:  regexp.MustCompile(`Acct XX(?P<account>[0-9]{3,4}) credited with Rs ?(?P<amount>[0-9,]+\.[0-9]{2}) on (?P<date>[0-9]{2}-[A-Za-z]{3}-[0-9]{2}) from (?P<payer>\S+?)\. UPI:(?P<utr>[0-9]{12})\b`),
	layout:  "02-Jan-06",
	extract: func(m map[string]string) (string, string, string) {
		return m["utr"], maskPayer(m["payer"]), m["account"]
	},
}

var axis = bank{
	name:    "Axis",
	senders: []*regexp.Regexp{sender("AXISBK")},
	domains: []string{"axisbank.com"},
	credit:  regexp.MustCompile(`INR (?P<amount>[0-9,]+\.[0-9]{2}) credited to A/c no\. XX(?P<account>[0-9]{4}) on (?P<date>[0-9]{2}-[0-9]{2}-[0-9]{4}) UPI/P2A/(?P<utr>[0-9]{12})\b/(?P<payer>[^/]+)/`),
	layout:  "02-01-2006",
	extract: func(m map[string]string) (string, string, string) {
		return m["utr"], maskName(m["payer"]), m["account"]
	},
}

var kotak = bank{
	name:    "Kotak",
	senders: []*regexp.Regexp{sender("KOTAKB")},
	domains: []string{"kotak.com"},
	credit:  regexp.MustCompile(`Received Rs\.(?P<amount>[0-9,]+\.[0-9]{2}) in your Kotak Bank AC X(?P<account>[0-9]{4}) from (?P<payer>\S+) on (?P<date>[0-9]{2}-[0-9]{2}-[0-9]{2})\.UPI Ref:(?P<utr>[0-9]{12})\b`),
	layout:  "02-01-06",
	extract: func(m map[string]string) (string, string, string) {
		return m["utr"], maskPayer(m["payer"]), m["account"]
	},
}
