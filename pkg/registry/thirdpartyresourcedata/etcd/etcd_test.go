/*
Copyright 2015 The Kubernetes Authors All rights reserved.

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

package etcd

import (
	"testing"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/rest/resttest"
	"k8s.io/kubernetes/pkg/api/testapi"
	"k8s.io/kubernetes/pkg/expapi"
	"k8s.io/kubernetes/pkg/expapi/v1"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/registry/registrytest"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/storage"
	etcdstorage "k8s.io/kubernetes/pkg/storage/etcd"
	"k8s.io/kubernetes/pkg/tools"
	"k8s.io/kubernetes/pkg/tools/etcdtest"

	"github.com/coreos/go-etcd/etcd"
)

var scheme *runtime.Scheme
var codec runtime.Codec

func init() {
	// Ensure that expapi/v1 packege is used, so that it will get initialized and register HorizontalPodAutoscaler object.
	_ = v1.ThirdPartyResourceData{}
}

func newStorage(t *testing.T) (*REST, *tools.FakeEtcdClient, storage.Interface) {
	fakeEtcdClient := tools.NewFakeEtcdClient(t)
	fakeEtcdClient.TestIndex = true
	etcdStorage := etcdstorage.NewEtcdStorage(fakeEtcdClient, testapi.Codec(), etcdtest.PathPrefix())
	storage := NewREST(etcdStorage, "foo", "bar")
	return storage, fakeEtcdClient, etcdStorage
}

func validNewThirdPartyResourceData(name string) *expapi.ThirdPartyResourceData {
	return &expapi.ThirdPartyResourceData{
		ObjectMeta: api.ObjectMeta{
			Name:      name,
			Namespace: api.NamespaceDefault,
		},
		Data: []byte("foobarbaz"),
	}
}

func TestCreate(t *testing.T) {
	storage, fakeEtcdClient, _ := newStorage(t)
	test := registrytest.New(t, fakeEtcdClient, storage.Etcd)
	rsrc := validNewThirdPartyResourceData("foo")
	rsrc.ObjectMeta = api.ObjectMeta{}
	test.TestCreate(
		// valid
		rsrc,
		// invalid
		&expapi.ThirdPartyResourceData{},
	)
}

func TestUpdate(t *testing.T) {
	storage, fakeClient, _ := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd)
	test.TestUpdate(
		// valid
		validNewThirdPartyResourceData("foo"),
		// updateFunc
		func(obj runtime.Object) runtime.Object {
			object := obj.(*expapi.ThirdPartyResourceData)
			object.Data = []byte("new description")
			return object
		},
	)
}

func TestGet(t *testing.T) {
	storage, fakeEtcdClient, _ := newStorage(t)
	test := resttest.New(t, storage, fakeEtcdClient.SetError)
	rsrc := validNewThirdPartyResourceData("foo")
	test.TestGet(rsrc)
}

func TestEmptyList(t *testing.T) {
	ctx := api.NewDefaultContext()
	registry, fakeClient, _ := newStorage(t)
	fakeClient.ChangeIndex = 1
	key := registry.KeyRootFunc(ctx)
	key = etcdtest.AddPrefix(key)
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{},
		E: fakeClient.NewError(tools.EtcdErrorCodeNotFound),
	}
	rsrcList, err := registry.List(ctx, labels.Everything(), fields.Everything())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rsrcList.(*expapi.ThirdPartyResourceDataList).Items) != 0 {
		t.Errorf("Unexpected non-zero autoscaler list: %#v", rsrcList)
	}
	if rsrcList.(*expapi.ThirdPartyResourceDataList).ResourceVersion != "1" {
		t.Errorf("Unexpected resource version: %#v", rsrcList)
	}
}

func TestList(t *testing.T) {
	ctx := api.NewDefaultContext()
	registry, fakeClient, _ := newStorage(t)
	fakeClient.ChangeIndex = 1
	key := registry.KeyRootFunc(ctx)
	key = etcdtest.AddPrefix(key)
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Nodes: []*etcd.Node{
					{
						Value: runtime.EncodeOrDie(testapi.Codec(), &expapi.ThirdPartyResourceData{
							ObjectMeta: api.ObjectMeta{Name: "foo"},
						}),
					},
					{
						Value: runtime.EncodeOrDie(testapi.Codec(), &expapi.ThirdPartyResourceData{
							ObjectMeta: api.ObjectMeta{Name: "bar"},
						}),
					},
				},
			},
		},
	}
	obj, err := registry.List(ctx, labels.Everything(), fields.Everything())
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	rsrcList := obj.(*expapi.ThirdPartyResourceDataList)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rsrcList.Items) != 2 {
		t.Errorf("Unexpected ThirdPartyResourceData list: %#v", rsrcList)
	}
	if rsrcList.Items[0].Name != "foo" {
		t.Errorf("Unexpected ThirdPartyResourceData: %#v", rsrcList.Items[0])
	}
	if rsrcList.Items[1].Name != "bar" {
		t.Errorf("Unexpected ThirdPartyResourceData: %#v", rsrcList.Items[1])
	}
}
