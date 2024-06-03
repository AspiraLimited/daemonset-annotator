package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/admission/v1"
	appsV1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

type WebhookConfig struct {
	Annotations map[string]string `yaml:"annotations"`
	Server      ServerParameters  `yaml:"server"`
}

type ServerParameters struct {
	CertFile string `yaml:"certificate"`
	KeyFile  string `yaml:"key"`
	Port     int    `yaml:"port"`
}

type patchOperation struct {
	Value interface{} `json:"value,omitempty"`
	Op    string      `json:"op"`
	Path  string      `json:"path"`
}

var (
	universalDeserializer = serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer()
	configPath            string
	config                *WebhookConfig
)

func init() {
	flag.StringVar(&configPath, "config", "/etc/webhook/config/config.yaml", "Path to the configuration file")
	flag.Parse()
}

func NewConfig() (*WebhookConfig, error) {
	config := &WebhookConfig{}

	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	d := yaml.NewDecoder(file)
	if err := d.Decode(config); err != nil {
		return nil, err
	}

	return config, nil
}

func HandleRoot(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Handleroot"))
}

func HandleMutate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	var admissionReviewReq v1.AdmissionReview

	if _, _, err := universalDeserializer.Decode(body, nil, &admissionReviewReq); err != nil {
		http.Error(w, fmt.Sprintf("could not deserialize request: %v", err), http.StatusBadRequest)
		return
	} else if admissionReviewReq.Request == nil {
		http.Error(w, "malformed admission review: request is nil", http.StatusBadRequest)
		return
	}

	fmt.Printf("Type: %v \t Event: %v \t Name: %v \n",
		admissionReviewReq.Request.Kind,
		admissionReviewReq.Request.Operation,
		admissionReviewReq.Request.Name,
	)

	var ds appsV1.DaemonSet

	err = json.Unmarshal(admissionReviewReq.Request.Object.Raw, &ds)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not unmarshal rs on admission request: %v", err), http.StatusBadRequest)
		return
	}

	var patches []patchOperation

	annotations := ds.Spec.Template.ObjectMeta.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}

	for key, value := range config.Annotations {
		annotations[key] = value
	}

	patches = append(patches, patchOperation{
		Op:    "add",
		Path:  "/spec/template/metadata/annotations",
		Value: annotations,
	})

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not marshal JSON patch: %v", err), http.StatusInternalServerError)
		return
	}

	admissionReviewResponse := v1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: &v1.AdmissionResponse{
			UID:     admissionReviewReq.Request.UID,
			Allowed: true,
			Patch:   patchBytes,
			PatchType: func() *v1.PatchType {
				pt := v1.PatchTypeJSONPatch
				return &pt
			}(),
		},
	}

	bytes, err := json.Marshal(&admissionReviewResponse)
	if err != nil {
		http.Error(w, fmt.Sprintf("marshaling response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Write(bytes)
}

func main() {
	fmt.Println("Starting webhook server")
	var err error
	config, err = NewConfig()
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}
	fmt.Println("Config: ", config.Server.Port, config.Server.CertFile, config.Server.KeyFile)

	http.HandleFunc("/", HandleRoot)
	http.HandleFunc("/mutate", HandleMutate)
	log.Fatal(http.ListenAndServeTLS(":"+strconv.Itoa(config.Server.Port), config.Server.CertFile, config.Server.KeyFile, nil))
}
