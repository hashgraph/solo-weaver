package platform

const LogNameSpacePlatform = "platform"

// logFields defines various default log field key names
var logFields = struct {
	logPrefix              string
	installPath            string
	installDataPath        string
	extractedSDKPath       string
	extractedSDKDataPath   string
	extractedSDKKeysPath   string
	errorCode              string
	extractedSDKConfigPath string
	hederaCertFilePath     string
	hederaKeyFilePath      string
	filePath               string
	fileName               string
}{
	logPrefix:              "log_prefix",
	installPath:            "install_path",
	installDataPath:        "install_data_path",
	extractedSDKPath:       "extracted_sdk_path",
	extractedSDKDataPath:   "extracted_sdk_data_path",
	extractedSDKKeysPath:   "extracted_sdk_keys_path",
	errorCode:              "error_code",
	extractedSDKConfigPath: "extracted_sdk_config_path",
	hederaCertFilePath:     "hedera_cert_file_path",
	hederaKeyFilePath:      "hedera_key_file_path",
	filePath:               "file_path",
	fileName:               "file_name",
}
