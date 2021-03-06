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

	"github.com/coreos/go-etcd/etcd"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/rest/resttest"
	"k8s.io/kubernetes/pkg/api/testapi"
	"k8s.io/kubernetes/pkg/expapi"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/registry/registrytest"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/tools"
	"k8s.io/kubernetes/pkg/tools/etcdtest"
)

func newStorage(t *testing.T) (*REST, *tools.FakeEtcdClient) {
	etcdStorage, fakeClient := registrytest.NewEtcdStorage(t)
	return NewREST(etcdStorage), fakeClient
}

func validNewDaemon() *expapi.Daemon {
	return &expapi.Daemon{
		ObjectMeta: api.ObjectMeta{
			Name:      "foo",
			Namespace: api.NamespaceDefault,
		},
		Spec: expapi.DaemonSpec{
			Selector: map[string]string{"a": "b"},
			Template: &api.PodTemplateSpec{
				ObjectMeta: api.ObjectMeta{
					Labels: map[string]string{"a": "b"},
				},
				Spec: api.PodSpec{
					Containers: []api.Container{
						{
							Name:            "test",
							Image:           "test_image",
							ImagePullPolicy: api.PullIfNotPresent,
						},
					},
					RestartPolicy: api.RestartPolicyAlways,
					DNSPolicy:     api.DNSClusterFirst,
				},
			},
		},
	}
}

var validDaemon = validNewDaemon()

func TestCreate(t *testing.T) {
	storage, fakeClient := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd)
	daemon := validNewDaemon()
	daemon.ObjectMeta = api.ObjectMeta{}
	test.TestCreate(
		// valid
		daemon,
		// invalid (invalid selector)
		&expapi.Daemon{
			Spec: expapi.DaemonSpec{
				Selector: map[string]string{},
				Template: validDaemon.Spec.Template,
			},
		},
	)
}

func TestUpdate(t *testing.T) {
	storage, fakeClient := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd)
	test.TestUpdate(
		// valid
		validNewDaemon(),
		// updateFunc
		func(obj runtime.Object) runtime.Object {
			object := obj.(*expapi.Daemon)
			object.Spec.Template.Spec.NodeSelector = map[string]string{"c": "d"}
			return object
		},
		// invalid updateFunc
		func(obj runtime.Object) runtime.Object {
			object := obj.(*expapi.Daemon)
			object.UID = "newUID"
			return object
		},
		func(obj runtime.Object) runtime.Object {
			object := obj.(*expapi.Daemon)
			object.Name = ""
			return object
		},
		func(obj runtime.Object) runtime.Object {
			object := obj.(*expapi.Daemon)
			object.Spec.Template.Spec.RestartPolicy = api.RestartPolicyOnFailure
			return object
		},
		func(obj runtime.Object) runtime.Object {
			object := obj.(*expapi.Daemon)
			object.Spec.Selector = map[string]string{}
			return object
		},
	)
}

func TestGet(t *testing.T) {
	storage, fakeClient := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd)
	test.TestGet(validNewDaemon())
}

func TestList(t *testing.T) {
	storage, fakeClient := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd)
	test.TestList(validNewDaemon())
}

func TestWatch(t *testing.T) {
	storage, fakeClient := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd)
	test.TestWatch(
		validDaemon,
		// matching labels
		[]labels.Set{
			{"a": "b"},
		},
		// not matching labels
		[]labels.Set{
			{"a": "c"},
			{"foo": "bar"},
		},
		// matching fields
		[]fields.Set{
			{"metadata.name": "foo"},
		},
		// notmatching fields
		[]fields.Set{
			{"metadata.name": "bar"},
			{"name": "foo"},
		},
	)
}

func TestEtcdDeleteDaemon(t *testing.T) {
	ctx := api.NewDefaultContext()
	storage, fakeClient := newStorage(t)
	key, err := storage.KeyFunc(ctx, validDaemon.Name)
	key = etcdtest.AddPrefix(key)

	fakeClient.Set(key, runtime.EncodeOrDie(testapi.Codec(), validDaemon), 0)
	obj, err := storage.Delete(ctx, validDaemon.Name, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if status, ok := obj.(*api.Status); !ok {
		t.Errorf("Expected status of delete, got %#v", status)
	} else if status.Status != api.StatusSuccess {
		t.Errorf("Expected success, got %#v", status.Status)
	}
	if len(fakeClient.DeletedKeys) != 1 {
		t.Errorf("Expected 1 delete, found %#v", fakeClient.DeletedKeys)
	}
	if fakeClient.DeletedKeys[0] != key {
		t.Errorf("Unexpected key: %s, expected %s", fakeClient.DeletedKeys[0], key)
	}
}

func TestDelete(t *testing.T) {
	ctx := api.NewDefaultContext()
	storage, fakeClient := newStorage(t)
	test := resttest.New(t, storage, fakeClient.SetError)
	key, _ := storage.KeyFunc(ctx, validDaemon.Name)
	key = etcdtest.AddPrefix(key)

	createFn := func() runtime.Object {
		dc := validNewDaemon()
		dc.ResourceVersion = "1"
		fakeClient.Data[key] = tools.EtcdResponseWithError{
			R: &etcd.Response{
				Node: &etcd.Node{
					Value:         runtime.EncodeOrDie(testapi.Codec(), dc),
					ModifiedIndex: 1,
				},
			},
		}
		return dc
	}
	gracefulSetFn := func() bool {
		// If the daemon is still around after trying to delete either the delete
		// failed, or we're deleting it gracefully.
		if fakeClient.Data[key].R.Node != nil {
			return true
		}
		return false
	}

	test.TestDelete(createFn, gracefulSetFn)
}
