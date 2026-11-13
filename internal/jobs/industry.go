package jobs

import (
	"regexp"
	"strings"
)

// Industry searches. Crypto companies post plain titles ("Software
// Engineer", "Account Executive"), so a "crypto jobs" search has to know
// which employers are crypto companies. Their jobs are saved with a
// "crypto" tag in the searchable title (TitleSearchText), and a search
// that names the industry keeps only jobs at those employers, or jobs
// whose title names it ("Blockchain Engineer").

// cryptoCompanies are crypto and web3 employers, by compactText name.
var cryptoCompanies = map[string]struct{}{
	"alchemy":            {},
	"anchoragedigital":   {},
	"animocabrands":      {},
	"aptoslabs":          {},
	"binance":            {},
	"bitfinex":           {},
	"bitgo":              {},
	"blockstream":        {},
	"blockworks":         {},
	"bullish":            {},
	"bybit":              {},
	"casa":               {},
	"certik":             {},
	"chainalysis":        {},
	"chainlinklabs":      {},
	"circle":             {},
	"coinbase":           {},
	"coingecko":          {},
	"coinmarketcap":      {},
	"consensys":          {},
	"cryptocom":          {},
	"elliptic":           {},
	"ethereumfoundation": {},
	"fireblocks":         {},
	"galaxydigital":      {},
	"gauntlet":           {},
	"gemini":             {},
	"geminitrust":        {},
	"kraken":             {},
	"ledger":             {},
	"metamask":           {},
	"moonpay":            {},
	"mystenlabs":         {},
	"offchainlabs":       {},
	"okx":                {},
	"ondofinance":        {},
	"openzeppelin":       {},
	"oplabs":             {},
	"paradigm":           {},
	"paxos":              {},
	"phantom":            {},
	"polymarket":         {},
	"ripple":             {},
	"securitize":         {},
	"skymavis":           {},
	"solanafoundation":   {},
	"strike":             {},
	"taxbit":             {},
	"tether":             {},
	"turnkey":            {},
	"uniswaplabs":        {},
	"uphold":             {},
}

// industryWords are what people type for the crypto industry.
var industryWords = map[string]string{
	"crypto": "crypto", "cryptocurrency": "crypto", "cryptocurrencies": "crypto", "blockchain": "crypto",
	"web3": "crypto", "defi": "crypto", "bitcoin": "crypto", "ethereum": "crypto", "nft": "crypto",
	"nfts": "crypto", "onchain": "crypto", "solidity": "crypto", "dao": "crypto",
}

var cryptoTitleWords = regexp.MustCompile(`(?i)\b(crypto\w*|blockchain|web3|defi|bitcoin|ethereum|nfts?|on-?chain|solidity|smart contracts?|tokenization|digital assets?)\b`)

// companyIndustry is the industry Jane knows an employer is in.
func companyIndustry(company string) string {
	if _, ok := cryptoCompanies[compactText(company)]; ok {
		return "crypto"
	}
	return ""
}

// TitleSearchText is what a saved job's title is searched by: the
// lowercased title, plus its employer's industry.
func TitleSearchText(title, company string) string {
	text := strings.ToLower(title)
	if industry := companyIndustry(company); industry != "" {
		text += " | " + industry
	}
	return text
}

// splitIndustry takes the industry out of a search: "crypto software
// engineer jobs" is the crypto industry plus "software engineer".
func splitIndustry(role string) (industry, rest string) {
	words := strings.Fields(cleanSearchText(role))
	kept := words[:0]
	for _, word := range words {
		if found, ok := industryWords[word]; ok {
			industry = found
			continue
		}
		kept = append(kept, word)
	}
	return industry, strings.Join(kept, " ")
}

// jobInIndustry reports a job at an employer in the industry, or one whose
// title names it.
func jobInIndustry(industry string, job Job) bool {
	if industry == "" {
		return true
	}
	return companyIndustry(job.Company) == industry || (industry == "crypto" && cryptoTitleWords.MatchString(job.Title))
}
