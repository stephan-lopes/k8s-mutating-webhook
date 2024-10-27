package main

import (
	"encoding/json"
	"log"
	"net/http"

	v1 "k8s.io/api/core/v1"

	"github.com/mattbaird/jsonpatch"
	admission "k8s.io/api/admission/v1"
)

func main() {
	http.HandleFunc("/mutate", mutate)
	log.Println("Starting webhook server...")
	log.Fatal(http.ListenAndServeTLS(":443", "/etc/certs/tls.crt", "/etc/certs/tls.key", nil))
}

func mutate(w http.ResponseWriter, r *http.Request) {
	var admissionReview admission.AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&admissionReview); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}

	if admissionReview.Request.Kind.Kind != "Pod" || admissionReview.Request.Operation != admission.Create {
		response(w, admissionReview, nil)
		return
	}

	if admissionReview.Request.Namespace != "kube-system" {
		response(w, admissionReview, nil)
		return
	}

	raw := admissionReview.Request.Object.Raw
	pod := v1.Pod{}
	if err := json.Unmarshal(raw, &pod); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	originalPod := pod.DeepCopy()

	if pod.Spec.Tolerations == nil {
		pod.Spec.Tolerations = []v1.Toleration{}
	}

	toleration := v1.Toleration{
		Key:      "kube-system-pool",
		Operator: v1.TolerationOpEqual,
		Value:    "true",
		Effect:   v1.TaintEffectNoSchedule,
	}

	exists := false
	for _, t := range pod.Spec.Tolerations {
		if t.Key == "kube-system-pool" {
			exists = true
			break
		}
	}

	if !exists {
		pod.Spec.Tolerations = append(pod.Spec.Tolerations, toleration)
	}

	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = &v1.Affinity{}
	}

	if pod.Spec.Affinity.NodeAffinity == nil {
		pod.Spec.Affinity.NodeAffinity = &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      "doks.digitalocean.com/node-pool",
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{"kube-system"},
							},
						},
					},
				},
			},
		}
	} else {
		expressionExists := false
		for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == "doks.digitalocean.com/node-pool" {
					expressionExists = true
					break
				}
			}
		}

		if !expressionExists {
			pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = append(
				pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms,
				v1.NodeSelectorTerm{
					MatchExpressions: []v1.NodeSelectorRequirement{
						{
							Key:      "doks.digitalocean.com/node-pool",
							Operator: v1.NodeSelectorOpIn,
							Values:   []string{"kube-system"},
						},
					},
				},
			)
		}
	}

	patch, err := createPatch(originalPod, &pod)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Generated Patch: %s", string(patch))

	response(w, admissionReview, patch)
}

func createPatch(originalPod *v1.Pod, modifiedPod *v1.Pod) ([]byte, error) {
	original, err := json.Marshal(originalPod)
	if err != nil {
		return nil, err
	}

	modified, err := json.Marshal(modifiedPod)
	if err != nil {
		return nil, err
	}

	patch, err := jsonpatch.CreatePatch(original, modified)
	if err != nil {
		return nil, err
	}

	return json.Marshal(patch)
}

func response(w http.ResponseWriter, admissionReview admission.AdmissionReview, patch []byte) {
	response := admission.AdmissionReview{
		TypeMeta: admissionReview.TypeMeta,
		Response: &admission.AdmissionResponse{
			UID:     admissionReview.Request.UID,
			Allowed: true,
		},
	}

	if patch != nil {
		patchType := admission.PatchTypeJSONPatch
		response.Response.Patch = patch
		response.Response.PatchType = &patchType
		response.Response.Allowed = true
	}

	rb, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(rb)
}
