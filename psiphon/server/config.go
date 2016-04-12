/*
 * Copyright (c) 2016, Psiphon Inc.
 * All rights reserved.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/Psiphon-Labs/psiphon-tunnel-core/psiphon"
	"golang.org/x/crypto/ssh"
)

const (
	SERVER_CONFIG_FILENAME                 = "psiphon-server.config"
	SERVER_ENTRY_FILENAME                  = "serverEntry.dat"
	DEFAULT_LOG_LEVEL                      = "Info"
	DEFAULT_GEO_IP_DATABASE_FILENAME       = "GeoLite2-City.mmdb"
	DEFAULT_SERVER_IP_ADDRESS              = "127.0.0.1"
	WEB_SERVER_SECRET_BYTE_LENGTH          = 32
	WEB_SERVER_CERTIFICATE_RSA_KEY_BITS    = 2048
	WEB_SERVER_CERTIFICATE_VALIDITY_PERIOD = 10 * 365 * 24 * time.Hour // approx. 10 years
	DEFAULT_WEB_SERVER_PORT                = 8000
	WEB_SERVER_READ_TIMEOUT                = 10 * time.Second
	WEB_SERVER_WRITE_TIMEOUT               = 10 * time.Second
	SSH_USERNAME_SUFFIX_BYTE_LENGTH        = 8
	SSH_PASSWORD_BYTE_LENGTH               = 32
	SSH_RSA_HOST_KEY_BITS                  = 2048
	DEFAULT_SSH_SERVER_PORT                = 2222
	SSH_HANDSHAKE_TIMEOUT                  = 30 * time.Second
	SSH_CONNECTION_READ_DEADLINE           = 5 * time.Minute
	SSH_OBFUSCATED_KEY_BYTE_LENGTH         = 32
	DEFAULT_OBFUSCATED_SSH_SERVER_PORT     = 3333
)

// TODO: break config into sections (sub-structs)

type Config struct {
	LogLevel                string
	SyslogAddress           string
	SyslogFacility          string
	SyslogTag               string
	GeoIPDatabaseFilename   string
	ServerIPAddress         string
	WebServerPort           int
	WebServerSecret         string
	WebServerCertificate    string
	WebServerPrivateKey     string
	SSHPrivateKey           string
	SSHServerVersion        string
	SSHUserName             string
	SSHPassword             string
	SSHServerPort           int
	ObfuscatedSSHKey        string
	ObfuscatedSSHServerPort int
}

func LoadConfig(configJson []byte) (*Config, error) {

	var config Config
	err := json.Unmarshal(configJson, &config)
	if err != nil {
		return nil, psiphon.ContextError(err)
	}

	// TODO: config field validation
	// TODO: validation case: OSSH requires extra fields

	return &config, nil
}

type GenerateConfigParams struct {
	ServerIPAddress         string
	WebServerPort           int
	SSHServerPort           int
	ObfuscatedSSHServerPort int
}

func GenerateConfig(params *GenerateConfigParams) ([]byte, []byte, error) {

	// TODO: support disabling web server or a subset of protocols

	serverIPaddress := params.ServerIPAddress
	if serverIPaddress == "" {
		serverIPaddress = DEFAULT_SERVER_IP_ADDRESS
	}

	// Web server config

	webServerPort := params.WebServerPort
	if webServerPort == 0 {
		webServerPort = DEFAULT_WEB_SERVER_PORT
	}

	webServerSecret, err := psiphon.MakeRandomString(WEB_SERVER_SECRET_BYTE_LENGTH)
	if err != nil {
		return nil, nil, psiphon.ContextError(err)
	}

	webServerCertificate, webServerPrivateKey, err := generateWebServerCertificate()
	if err != nil {
		return nil, nil, psiphon.ContextError(err)
	}

	// SSH config

	sshServerPort := params.SSHServerPort
	if sshServerPort == 0 {
		sshServerPort = DEFAULT_SSH_SERVER_PORT
	}

	// TODO: use other key types: anti-fingerprint by varying params

	rsaKey, err := rsa.GenerateKey(rand.Reader, SSH_RSA_HOST_KEY_BITS)
	if err != nil {
		return nil, nil, psiphon.ContextError(err)
	}

	sshPrivateKey := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
		},
	)

	signer, err := ssh.NewSignerFromKey(rsaKey)
	if err != nil {
		return nil, nil, psiphon.ContextError(err)
	}

	sshPublicKey := signer.PublicKey()

	sshUserNameSuffix, err := psiphon.MakeRandomString(SSH_USERNAME_SUFFIX_BYTE_LENGTH)
	if err != nil {
		return nil, nil, psiphon.ContextError(err)
	}

	sshUserName := "psiphon_" + sshUserNameSuffix

	sshPassword, err := psiphon.MakeRandomString(SSH_PASSWORD_BYTE_LENGTH)
	if err != nil {
		return nil, nil, psiphon.ContextError(err)
	}

	// TODO: vary version string for anti-fingerprint
	sshServerVersion := "SSH-2.0-Psiphon"

	// Obfuscated SSH config

	obfuscatedSSHServerPort := params.ObfuscatedSSHServerPort
	if obfuscatedSSHServerPort == 0 {
		obfuscatedSSHServerPort = DEFAULT_OBFUSCATED_SSH_SERVER_PORT
	}

	obfuscatedSSHKey, err := psiphon.MakeRandomString(SSH_OBFUSCATED_KEY_BYTE_LENGTH)
	if err != nil {
		return nil, nil, psiphon.ContextError(err)
	}

	// Assemble config and server entry

	config := &Config{
		LogLevel:                DEFAULT_LOG_LEVEL,
		SyslogAddress:           "",
		SyslogFacility:          "",
		SyslogTag:               "",
		GeoIPDatabaseFilename:   DEFAULT_GEO_IP_DATABASE_FILENAME,
		ServerIPAddress:         serverIPaddress,
		WebServerPort:           webServerPort,
		WebServerSecret:         webServerSecret,
		WebServerCertificate:    webServerCertificate,
		WebServerPrivateKey:     webServerPrivateKey,
		SSHPrivateKey:           string(sshPrivateKey),
		SSHServerVersion:        sshServerVersion,
		SSHUserName:             sshUserName,
		SSHPassword:             sshPassword,
		SSHServerPort:           sshServerPort,
		ObfuscatedSSHKey:        obfuscatedSSHKey,
		ObfuscatedSSHServerPort: obfuscatedSSHServerPort,
	}

	encodedConfig, err := json.MarshalIndent(config, "\n", "    ")
	if err != nil {
		return nil, nil, psiphon.ContextError(err)
	}

	// Server entry format omits the BEGIN/END lines and newlines
	lines := strings.Split(webServerCertificate, "\n")
	strippedWebServerCertificate := strings.Join(lines[1:len(lines)-2], "")

	capabilities := []string{
		psiphon.GetCapability(psiphon.TUNNEL_PROTOCOL_SSH),
		psiphon.GetCapability(psiphon.TUNNEL_PROTOCOL_OBFUSCATED_SSH),
	}

	serverEntry := &psiphon.ServerEntry{
		IpAddress:            serverIPaddress,
		WebServerPort:        fmt.Sprintf("%d", webServerPort),
		WebServerSecret:      webServerSecret,
		WebServerCertificate: strippedWebServerCertificate,
		SshPort:              sshServerPort,
		SshUsername:          sshUserName,
		SshPassword:          sshPassword,
		SshHostKey:           base64.RawStdEncoding.EncodeToString(sshPublicKey.Marshal()),
		SshObfuscatedPort:    obfuscatedSSHServerPort,
		SshObfuscatedKey:     obfuscatedSSHKey,
		Capabilities:         capabilities,
		Region:               "US",
	}

	encodedServerEntry, err := psiphon.EncodeServerEntry(serverEntry)
	if err != nil {
		return nil, nil, psiphon.ContextError(err)
	}

	return encodedConfig, []byte(encodedServerEntry), nil
}

func generateWebServerCertificate() (string, string, error) {

	// Based on https://golang.org/src/crypto/tls/generate_cert.go

	// TODO: use other key types: anti-fingerprint by varying params

	rsaKey, err := rsa.GenerateKey(rand.Reader, WEB_SERVER_CERTIFICATE_RSA_KEY_BITS)
	if err != nil {
		return "", "", psiphon.ContextError(err)
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(WEB_SERVER_CERTIFICATE_VALIDITY_PERIOD)

	// TODO: psi_ops_install sets serial number to 0?
	// TOSO: psi_ops_install sets RSA exponent to 3, digest type to 'sha1', and version to 2?

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return "", "", psiphon.ContextError(err)
	}

	template := x509.Certificate{

		// TODO: psi_ops_install leaves subject blank?
		/*
			Subject: pkix.Name{
				Organization: []string{""},
			},
			IPAddresses: ...
		*/

		SerialNumber:          serialNumber,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA: true,
	}

	derCert, err := x509.CreateCertificate(rand.Reader, &template, &template, rsaKey.Public(), rsaKey)
	if err != nil {
		return "", "", psiphon.ContextError(err)
	}

	webServerCertificate := pem.EncodeToMemory(
		&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: derCert,
		},
	)

	webServerPrivateKey := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
		},
	)

	return string(webServerCertificate), string(webServerPrivateKey), nil
}
