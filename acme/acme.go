package acme

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"

	"crypto/x509"

	"felix-hartmond.de/projects/certbutler/common"
	"golang.org/x/crypto/acme"
)

func loadAccount(ctx context.Context, accountFile string, acmeDirectory string) (*acme.Client, error) {
	akey, err := common.LoadKeyFromPEMFile(accountFile, 0)
	if err != nil {
		return nil, err
	}

	client := &acme.Client{Key: akey, DirectoryURL: acmeDirectory}
	_, err = client.GetReg(ctx, "")

	return client, err
}

func registerAccount(ctx context.Context, accountFile string, acmeDirectory string) (*acme.Client, error) {
	akey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	client := &acme.Client{Key: akey, DirectoryURL: acmeDirectory}
	_, err = client.Register(ctx, &acme.Account{}, acme.AcceptTOS)
	if err != nil {
		return nil, err
	}

	err = common.SaveToPEMFile(accountFile, akey, nil)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func RequestCertificate(dnsNames []string, accountFile string, certFile string, mustStaple bool, acmeDirectory string, registerIfMissing bool) error {
	ctx := context.Background()
	var client *acme.Client
	var err error

	client, err = loadAccount(ctx, accountFile, acmeDirectory)
	if err != nil {
		if !registerIfMissing {
			return err
		}
		client, err = registerAccount(ctx, accountFile, acmeDirectory)
		if err != nil {
			return err
		}
	}

	fmt.Println("Sending AuthorizeOrder Request")

	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(dnsNames...))
	if err != nil {
		return err
	}

	fmt.Println("Autorizing Domains")

	pendigChallenges := []*acme.Challenge{}
	dnsTokens := []string{}

	for _, authURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authURL)
		if err != nil {
			return err
		}

		if authz.Status == acme.StatusValid {
			fmt.Println(authz.Identifier.Value + " alredy autorized")
			// Already authorized.
			continue
		}

		var chal *acme.Challenge
		for _, c := range authz.Challenges {
			if c.Type == "dns-01" {
				chal = c
				break
			}
		}
		if chal == nil {
			return fmt.Errorf("no dns-01 challenge for %q", authURL)
		}

		val, err := client.DNS01ChallengeRecord(chal.Token)
		if err != nil {
			return fmt.Errorf("dns-01 token for %q: %v", authz.Identifier, err)
		}

		fmt.Printf("hosting dns challenge for %s: %s\n", authz.Identifier, val)

		dnsTokens = append(dnsTokens, val)
		pendigChallenges = append(pendigChallenges, chal)
	}

	if len(pendigChallenges) > 0 {
		// Preparing authorizsations - Start DNS server
		closeServer := hostDNS(dnsTokens)

		fmt.Println("Accepting pendig challanges")
		for _, chal := range pendigChallenges {
			if _, err := client.Accept(ctx, chal); err != nil {
				return fmt.Errorf("dns-01 accept for %q: %v", chal, err)
			}
		}

		fmt.Println("Waiting for authorizations...")
		for _, authURL := range order.AuthzURLs {
			if _, err := client.WaitAuthorization(ctx, authURL); err != nil {
				return fmt.Errorf("authorization for %q failed: %v", authURL, err)
			}
		}

		// Authorizations done - Stop DNS server
		closeServer <- true
	}

	fmt.Println("Generating PrivateKey and CSR")

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	// TODO add must staple if requested
	req := &x509.CertificateRequest{
		DNSNames: dnsNames,
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, req, key)
	if err != nil {
		return err
	}

	fmt.Println("Requesting Certificate")

	crts, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return err
	}

	fmt.Println("Saving Certificate")

	err = common.SaveToPEMFile(certFile, key, crts)
	if err != nil {
		return err
	}

	return nil
}