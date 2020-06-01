package scheduler

import (
	"fmt"
	"sync"
	"time"

	"felix-hartmond.de/projects/certbutler/acme"
	"felix-hartmond.de/projects/certbutler/common"
	"felix-hartmond.de/projects/certbutler/ocsp"
)

// RunConfig starts cerbutler tasked based on a configuration
func RunConfig(configs []common.Config) {
	wg := &sync.WaitGroup{}

	for _, config := range configs {
		if config.RunInteralMinutes == 0 {
			wg.Add(1)
			go func() {
				process(config)
				wg.Done()
			}()
		} else {
			ticker := time.NewTicker(time.Duration(config.RunInteralMinutes) * time.Minute)
			wg.Add(1) // this will never be set to done -> runs indefinitely
			go func(waitChannel <-chan time.Time, config common.Config) {
				for {
					process(config)
					<-waitChannel
				}
			}(ticker.C, config)
		}
	}
	wg.Wait()
}

func process(config common.Config) {
	renewCert, renewOCSP := getOpenTasks(config)

	fmt.Printf("%s starting run (cert: %t ocsp: %t)\n", time.Now(), renewCert, renewOCSP)

	if config.UpdateCert && renewCert {
		err := acme.RequestCertificate(config.DnsNames, config.AcmeAccountFile, config.CertFile, config.MustStaple, config.AcmeDirectory, config.RegsiterAcme)
		if err != nil {
			// request failed - TODO handle
			panic(err)
		}
	}

	if config.UpdateOCSP && renewOCSP {
		ocspResponse, err := ocsp.GetOcspResponse(config.CertFile)
		if err != nil {
			panic(err)
		}
		ocsp.PrintStatus(ocspResponse)
	}
}

func getOpenTasks(config common.Config) (renewCert, renewOCSP bool) {
	cert, err := common.LoadCertFromPEMFile(config.CertFile, 0)
	if err != nil {
		// no or invalid certificate => request cert
		return true, true
	}

	if remainingValidity := time.Until(cert.NotAfter); remainingValidity < time.Duration(14*24)*time.Hour {
		// cert will expire soon (in 2 weeks) => renwew cert
		return true, true
	}

	ocsp, err := ocsp.LoadFromFile(config.CertFile)
	if err != nil {
		// cert ok but ocsp missing or not valid => renew ocsp
		return false, true
	}

	if remainingValidity := time.Until(ocsp.NextUpdate); remainingValidity < time.Duration(3*24)*time.Hour {
		// cert ok but ocsp expires soon (in 3 days) => renew ocsp
		return false, true
	}

	// everythings fine => noting to do
	return false, false
}