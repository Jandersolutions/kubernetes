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
	"k8s.io/kubernetes/pkg/api/testapi"
	"k8s.io/kubernetes/pkg/registry/registrytest"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/tools"
	"k8s.io/kubernetes/pkg/tools/etcdtest"
	"k8s.io/kubernetes/pkg/util"

	"github.com/coreos/go-etcd/etcd"
)

func newStorage(t *testing.T) (*REST, *tools.FakeEtcdClient) {
	etcdStorage, fakeClient := registrytest.NewEtcdStorage(t)
	storage, _, _ := NewREST(etcdStorage)
	return storage, fakeClient
}

func validNewNamespace() *api.Namespace {
	return &api.Namespace{
		ObjectMeta: api.ObjectMeta{
			Name: "foo",
		},
	}
}

func TestCreate(t *testing.T) {
	storage, fakeClient := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd).ClusterScope()
	namespace := validNewNamespace()
	namespace.ObjectMeta = api.ObjectMeta{GenerateName: "foo"}
	test.TestCreate(
		// valid
		namespace,
		// invalid
		&api.Namespace{
			ObjectMeta: api.ObjectMeta{Name: "bad value"},
		},
	)
}

func expectNamespace(t *testing.T, out runtime.Object) (*api.Namespace, bool) {
	namespace, ok := out.(*api.Namespace)
	if !ok || namespace == nil {
		t.Errorf("Expected an api.Namespace object, was %#v", out)
		return nil, false
	}
	return namespace, true
}

func TestCreateSetsFields(t *testing.T) {
	storage, fakeClient := newStorage(t)
	namespace := validNewNamespace()
	ctx := api.NewContext()
	_, err := storage.Create(ctx, namespace)
	if err != fakeClient.Err {
		t.Fatalf("unexpected error: %v", err)
	}

	object, err := storage.Get(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	actual := object.(*api.Namespace)
	if actual.Name != namespace.Name {
		t.Errorf("unexpected namespace: %#v", actual)
	}
	if len(actual.UID) == 0 {
		t.Errorf("expected namespace UID to be set: %#v", actual)
	}
	if actual.Status.Phase != api.NamespaceActive {
		t.Errorf("expected namespace phase to be set to active, but %v", actual.Status.Phase)
	}
}

func TestNamespaceDecode(t *testing.T) {
	storage, _ := newStorage(t)
	expected := validNewNamespace()
	expected.Status.Phase = api.NamespaceActive
	expected.Spec.Finalizers = []api.FinalizerName{api.FinalizerKubernetes}
	body, err := testapi.Codec().Encode(expected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	actual := storage.New()
	if err := testapi.Codec().DecodeInto(body, actual); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !api.Semantic.DeepEqual(expected, actual) {
		t.Errorf("mismatch: %s", util.ObjectDiff(expected, actual))
	}
}

func TestGet(t *testing.T) {
	storage, fakeClient := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd).ClusterScope()
	test.TestGet(validNewNamespace())
}

func TestList(t *testing.T) {
	storage, fakeClient := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd).ClusterScope()
	test.TestList(validNewNamespace())
}

func TestDeleteNamespace(t *testing.T) {
	storage, fakeClient := newStorage(t)
	fakeClient.ChangeIndex = 1
	ctx := api.NewContext()
	key, err := storage.Etcd.KeyFunc(ctx, "foo")
	key = etcdtest.AddPrefix(key)
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Value: runtime.EncodeOrDie(testapi.Codec(), &api.Namespace{
					ObjectMeta: api.ObjectMeta{
						Name: "foo",
					},
					Status: api.NamespaceStatus{Phase: api.NamespaceActive},
				}),
				ModifiedIndex: 1,
				CreatedIndex:  1,
			},
		},
	}
	_, err = storage.Delete(api.NewContext(), "foo", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteNamespaceWithIncompleteFinalizers(t *testing.T) {
	storage, fakeClient := newStorage(t)
	fakeClient.ChangeIndex = 1
	key := etcdtest.AddPrefix("/namespaces/foo")
	now := util.Now()
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Value: runtime.EncodeOrDie(testapi.Codec(), &api.Namespace{
					ObjectMeta: api.ObjectMeta{
						Name:              "foo",
						DeletionTimestamp: &now,
					},
					Spec: api.NamespaceSpec{
						Finalizers: []api.FinalizerName{api.FinalizerKubernetes},
					},
					Status: api.NamespaceStatus{Phase: api.NamespaceActive},
				}),
				ModifiedIndex: 1,
				CreatedIndex:  1,
			},
		},
	}
	_, err := storage.Delete(api.NewContext(), "foo", nil)
	if err == nil {
		t.Fatalf("expected error: %v", err)
	}
}

func TestDeleteNamespaceWithCompleteFinalizers(t *testing.T) {
	storage, fakeClient := newStorage(t)
	fakeClient.ChangeIndex = 1
	key := etcdtest.AddPrefix("/namespaces/foo")
	now := util.Now()
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Value: runtime.EncodeOrDie(testapi.Codec(), &api.Namespace{
					ObjectMeta: api.ObjectMeta{
						Name:              "foo",
						DeletionTimestamp: &now,
					},
					Spec: api.NamespaceSpec{
						Finalizers: []api.FinalizerName{},
					},
					Status: api.NamespaceStatus{Phase: api.NamespaceActive},
				}),
				ModifiedIndex: 1,
				CreatedIndex:  1,
			},
		},
	}
	_, err := storage.Delete(api.NewContext(), "foo", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
