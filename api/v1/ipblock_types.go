/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// IPBlockSpec defines the desired state of IPBlock.
// 封禁请求
type IPBlockSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of IPBlock. Edit ipblock_types.go to remove/update
	Foo      string   `json:"foo,omitempty"`
	IP       string   `json:"ip"`                 // 目标IP
	Reason   string   `json:"reason,omitempty"`   // 封禁原因
	Source   string   `json:"source,omitempty"`   // 封禁来源，如 "alertmanager"、"manual"、"webhook"，便于追踪
	By       string   `json:"by,omitempty"`       // 谁触发的封禁
	Duration string   `json:"duration,omitempty"` // 封禁持续时间
	Tags     []string `json:"tags,omitempty"`     // 关键词筛选
	Unblock  bool     `json:"unblock,omitempty"`  // 用户显式解封
	Trigger  bool     `json:"trigger,omitempty"`  // 用户显式请求重新封禁

}

// IPBlockStatus defines the observed state of IPBlock.
// 封禁状态
type IPBlockStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Result       string `json:"result,omitempty"` // success, failed, unblocked
	Phase        string `json:"phase,omitempty"`  // pending, active, expired
	BlockedAt    string `json:"blockedAt,omitempty"`
	UnblockedAt  string `json:"unblockedAt,omitempty"`
	Message      string `json:"message,omitempty"`
	LastSpecHash string `json:"lastSpecHash,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// IPBlock is the Schema for the ipblocks API.
type IPBlock struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IPBlockSpec   `json:"spec,omitempty"`
	Status IPBlockStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// IPBlockList contains a list of IPBlock.
type IPBlockList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPBlock `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPBlock{}, &IPBlockList{})
}
