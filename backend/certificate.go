/*
 * @Copyright Reserved By Janusec (https://www.janusec.com/).
 * @Author: U2
 * @Date: 2018-07-14 16:21:46
 * @Last Modified: U2, 2018-07-14 16:21:46
 */

package backend

import (
	"crypto/tls"
	"errors"
	"log"

	"janusec/data"
	"janusec/models"
	"janusec/utils"
)

// Certs list
var Certs = []*models.CertItem{}

// LoadCerts ...
func LoadCerts() {
	//fmt.Println("LoadCerts")
	if data.IsPrimary {
		Certs = Certs[0:0]
		dbCerts := data.DAL.SelectCertificates()
		for _, dbCert := range dbCerts {
			cert := &models.CertItem{}
			cert.ID = dbCert.ID
			cert.CommonName = dbCert.CommonName
			cert.CertContent = dbCert.CertContent
			pubCert := []byte(cert.CertContent)
			privKey, err := data.AES256Decrypt(dbCert.EncryptedPrivKey, false)
			utils.CheckError("LoadCerts AES256Decrypt", err)
			tlsCert, err := tls.X509KeyPair(pubCert, privKey)
			utils.CheckError("LoadCerts X509KeyPair", err)
			cert.PrivKeyContent = string(privKey)
			cert.TlsCert = tlsCert
			cert.ExpireTime = dbCert.ExpireTime
			if dbCert.Description.Valid == true {
				cert.Description = dbCert.Description.String
			} else {
				cert.Description = ""
			}
			Certs = append(Certs, cert)
		}
	} else {
		// Replica
		rpcCerts := RPCSelectCertificates()
		if rpcCerts != nil {
			Certs = rpcCerts
		}
		//fmt.Println("LoadCerts Replica:", Certs)
	}
}

// GetCertificateByDomain ...
func GetCertificateByDomain(domain string) (*tls.Certificate, error) {
	if domainRelation, ok := DomainsMap.Load(domain); ok == true {
		certItem := domainRelation.(models.DomainRelation).Cert
		if certItem == nil {
			return nil, errors.New("GetCertificateByDomain Null CertItem: " + domain)
		}
		cert := &(certItem.TlsCert)
		return cert, nil
	}
	return nil, errors.New("Unknown Host: " + domain)
}

// GetCertificates ...
func GetCertificates(authUser *models.AuthUser) ([]*models.CertItem, error) {
	if authUser.IsCertAdmin == true {
		return Certs, nil
	}
	// Remove private key
	var simpleCerts = []*models.CertItem{}
	for _, cert := range Certs {
		simpleCert := &models.CertItem{
			ID:             cert.ID,
			CommonName:     cert.CommonName,
			CertContent:    "",
			PrivKeyContent: "You have no privilege to view the private key.",
			ExpireTime:     cert.ExpireTime,
			Description:    cert.Description,
		}
		simpleCerts = append(simpleCerts, simpleCert)
	}
	return simpleCerts, nil
}

// SysCallGetCertByID ... Use for internal call, not for UI
func SysCallGetCertByID(certID int64) (*models.CertItem, error) {
	for _, cert := range Certs {
		if cert.ID == certID {
			return cert, nil
		}
	}
	return nil, errors.New("Certificate not found")
}

// GetCertificateByID ...
func GetCertificateByID(certID int64, authUser *models.AuthUser) (*models.CertItem, error) {
	for _, cert := range Certs {
		if cert.ID == certID {
			if authUser.IsCertAdmin {
				return cert, nil
			}
			simpleCert := &models.CertItem{
				ID:             cert.ID,
				CommonName:     cert.CommonName,
				CertContent:    cert.CertContent,
				PrivKeyContent: "You have no privilege to view the private key.",
				ExpireTime:     cert.ExpireTime,
				Description:    cert.Description,
			}
			return simpleCert, nil
		}
	}
	return nil, errors.New("certificate id error")
}

// GetCertificateByCommonName ...
func GetCertificateByCommonName(commonName string) *models.CertItem {
	for _, cert := range Certs {
		if cert.CommonName == commonName {
			return cert
		}
	}
	log.Println("Get certificate err by common name:", commonName)
	return nil
}

// UpdateCertificate ...
func UpdateCertificate(param map[string]interface{}, authUser *models.AuthUser) (*models.CertItem, error) {
	certificate := param["object"].(map[string]interface{})
	id := int64(certificate["id"].(float64))
	commonName := certificate["common_name"].(string)
	certContent := certificate["cert_content"].(string)
	privKeyContent := certificate["priv_key_content"].(string)
	encryptedPrivKey := data.AES256Encrypt([]byte(privKeyContent), false)
	expireTime := data.GetCertificateExpiryTime(certContent)
	var description string
	var ok bool
	if description, ok = certificate["description"].(string); !ok {
		description = ""
	}
	var certItem *models.CertItem
	tlsCert, err := tls.X509KeyPair([]byte(certContent), []byte(privKeyContent))
	utils.CheckError("UpdateCertificate X509KeyPair", err)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		//new certificate
		newID := data.DAL.InsertCertificate(commonName, certContent, encryptedPrivKey, expireTime, description)
		certItem = &models.CertItem{}
		certItem.ID = newID
		Certs = append(Certs, certItem)
	} else {
		certItem, err = GetCertificateByID(id, authUser)
		if err != nil {
			return nil, err
		}
		err = data.DAL.UpdateCertificate(commonName, certContent, encryptedPrivKey, expireTime, description, id)
		if err != nil {
			return nil, err
		}
	}
	certItem.CommonName = commonName
	certItem.CertContent = certContent
	certItem.PrivKeyContent = privKeyContent
	certItem.TlsCert = tlsCert
	certItem.ExpireTime = expireTime
	certItem.Description = description
	data.UpdateBackendLastModified()
	return certItem, nil
}

// GetCertificateIndex ...
func GetCertificateIndex(certID int64) int {
	for i := 0; i < len(Certs); i++ {
		if Certs[i].ID == certID {
			return i
		}
	}
	return -1
}

// DeleteCertificateByID ...
func DeleteCertificateByID(certID int64) error {
	certDomainsCount := data.DAL.SelectDomainsCountByCertID(certID)
	if certDomainsCount > 0 {
		return errors.New("this certificate is in use, please delete relevant applications at first")
	}
	err := data.DAL.DeleteCertificate(certID)
	if err != nil {
		return err
	}
	i := GetCertificateIndex(certID)
	Certs = append(Certs[:i], Certs[i+1:]...)
	data.UpdateBackendLastModified()
	return nil
}
