package repository

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v2"
	"k8s.io/client-go/util/jsonpath"
)

func getValueFromYAMLByJSONPath(appPath, jsonPathExpression string) (*string, error) {
	// Чтение файла YAML
	content, err := os.ReadFile(appPath)
	if err != nil {
		return nil, err
	}

	// Разбор YAML-файла
	var obj interface{}
	if err := yaml.Unmarshal(content, &obj); err != nil {
		return nil, err
	}

	// Преобразование YAML в Map[interface{}]interface{} для работы jsonpath
	jsonObj, err := convertToJSONCompatible(obj)
	if err != nil {
		return nil, err
	}

	// Использование jsonpath для получения значения
	jp := jsonpath.New("jsonpathParser")
	jp.AllowMissingKeys(true)
	if err := jp.Parse(jsonPathExpression); err != nil {
		return nil, err
	}

	var buf strings.Builder
	err = jp.Execute(&buf, jsonObj)
	if err != nil {
		return nil, err
	}

	appVersion := buf.String()
	return &appVersion, nil
}

func convertToJSONCompatible(i interface{}) (interface{}, error) {
	data, err := yaml.Marshal(i)
	if err != nil {
		return nil, err
	}
	var obj interface{}
	if err := yaml.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func getAppVersion(appPath string, resourceName string, jsonPathExpression string) (*string, error) {
	// appPath = "example.yaml"
	// jsonPathExpression = "{.some.json.path}"

	value, err := getValueFromYAMLByJSONPath(appPath+"/"+resourceName, jsonPathExpression)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Value: %v\n", *value)

	return value, nil
}
