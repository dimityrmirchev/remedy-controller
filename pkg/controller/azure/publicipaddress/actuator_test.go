// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package publicipaddress_test

import (
	"context"
	"time"

	azurev1alpha1 "github.wdf.sap.corp/kubernetes/remedy-controller/pkg/apis/azure/v1alpha1"
	"github.wdf.sap.corp/kubernetes/remedy-controller/pkg/apis/config"
	"github.wdf.sap.corp/kubernetes/remedy-controller/pkg/controller"
	"github.wdf.sap.corp/kubernetes/remedy-controller/pkg/controller/azure/publicipaddress"
	mockclient "github.wdf.sap.corp/kubernetes/remedy-controller/pkg/mock/controller-runtime/client"
	mockprometheus "github.wdf.sap.corp/kubernetes/remedy-controller/pkg/mock/prometheus"
	mockutilsazure "github.wdf.sap.corp/kubernetes/remedy-controller/pkg/mock/remedy-controller/utils/azure"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2018-11-01/network"
	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
)

var _ = Describe("Actuator", func() {
	const (
		serviceName              = "test-service"
		namespace                = "test"
		ip                       = "1.2.3.4"
		azurePublicIPAddressID   = "/subscriptions/xxx/resourceGroups/shoot--dev--test/providers/Microsoft.Network/publicIPAddresses/shoot--dev--test-ip1"
		azurePublicIPAddressName = "shoot--dev--test-ip1"
	)

	var (
		ctrl *gomock.Controller
		ctx  context.Context

		c                 *mockclient.MockClient
		sw                *mockclient.MockStatusWriter
		pubipUtils        *mockutilsazure.MockPublicIPAddressUtils
		cleanedIPsCounter *mockprometheus.MockCounter

		cfg      config.AzurePublicIPRemedyConfiguration
		logger   logr.Logger
		actuator controller.Actuator

		pubip                *azurev1alpha1.PublicIPAddress
		pubipWithStatus      *azurev1alpha1.PublicIPAddress
		azurePublicIPAddress *network.PublicIPAddress
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ctx = context.TODO()

		c = mockclient.NewMockClient(ctrl)
		sw = mockclient.NewMockStatusWriter(ctrl)
		c.EXPECT().Status().Return(sw).AnyTimes()
		pubipUtils = mockutilsazure.NewMockPublicIPAddressUtils(ctrl)
		cleanedIPsCounter = mockprometheus.NewMockCounter(ctrl)

		cfg = config.AzurePublicIPRemedyConfiguration{
			RequeueInterval:     metav1.Duration{Duration: 1 * time.Second},
			DeletionGracePeriod: metav1.Duration{Duration: 1 * time.Second},
		}
		logger = log.Log.WithName("test")
		actuator = publicipaddress.NewActuator(pubipUtils, cfg, logger, cleanedIPsCounter)
		Expect(actuator.(inject.Client).InjectClient(c)).To(Succeed())

		pubip = &azurev1alpha1.PublicIPAddress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName + "-" + ip,
				Namespace: namespace,
			},
			Spec: azurev1alpha1.PublicIPAddressSpec{
				IPAddress: ip,
			},
		}
		pubipWithStatus = &azurev1alpha1.PublicIPAddress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName + "-" + ip,
				Namespace: namespace,
			},
			Spec: azurev1alpha1.PublicIPAddressSpec{
				IPAddress: ip,
			},
			Status: azurev1alpha1.PublicIPAddressStatus{
				Exists:            true,
				ID:                pointer.StringPtr(azurePublicIPAddressID),
				Name:              pointer.StringPtr(azurePublicIPAddressName),
				ProvisioningState: pointer.StringPtr(string(network.Succeeded)),
			},
		}
		azurePublicIPAddress = &network.PublicIPAddress{
			ID:   pointer.StringPtr(azurePublicIPAddressID),
			Name: pointer.StringPtr(azurePublicIPAddressName),
			PublicIPAddressPropertiesFormat: &network.PublicIPAddressPropertiesFormat{
				IPAddress:         pointer.StringPtr(ip),
				ProvisioningState: pointer.StringPtr(string(network.Succeeded)),
			},
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("#CreateOrUpdate", func() {
		It("should update the PublicIPAddress object status if the IP is found", func() {
			pubipUtils.EXPECT().GetByIP(ctx, ip).Return(azurePublicIPAddress, nil)
			c.EXPECT().Get(ctx, client.ObjectKey{Namespace: pubip.Namespace, Name: pubip.Name}, pubip).Return(nil)
			sw.EXPECT().Update(ctx, pubipWithStatus).Return(nil)

			requeueAfter, removeFinalizer, err := actuator.CreateOrUpdate(ctx, pubip)
			Expect(err).NotTo(HaveOccurred())
			Expect(requeueAfter).To(Equal(time.Duration(0)))
			Expect(removeFinalizer).To(Equal(false))
		})

		It("should not update the PublicIPAddress object status if the IP is not found", func() {
			pubipUtils.EXPECT().GetByIP(ctx, ip).Return(nil, nil)
			c.EXPECT().Get(ctx, client.ObjectKey{Namespace: pubip.Namespace, Name: pubip.Name}, pubip).Return(nil)

			requeueAfter, removeFinalizer, err := actuator.CreateOrUpdate(ctx, pubip)
			Expect(err).NotTo(HaveOccurred())
			Expect(requeueAfter).To(Equal(1 * time.Second))
			Expect(removeFinalizer).To(Equal(false))
		})

		It("should not update the PublicIPAddress object status if the IP is found and the status is already initialized", func() {
			pubipUtils.EXPECT().GetByName(ctx, azurePublicIPAddressName).Return(azurePublicIPAddress, nil)
			c.EXPECT().Get(ctx, client.ObjectKey{Namespace: pubipWithStatus.Namespace, Name: pubipWithStatus.Name}, pubipWithStatus).Return(nil)

			requeueAfter, removeFinalizer, err := actuator.CreateOrUpdate(ctx, pubipWithStatus)
			Expect(err).NotTo(HaveOccurred())
			Expect(requeueAfter).To(Equal(time.Duration(0)))
			Expect(removeFinalizer).To(Equal(false))
		})

		It("should fail if getting an Azure IP address by IP fails", func() {
			pubipUtils.EXPECT().GetByIP(ctx, ip).Return(nil, errors.New("test"))

			_, _, err := actuator.CreateOrUpdate(ctx, pubip)
			Expect(err).To(MatchError("could not get Azure public IP address by IP: test"))
		})

		It("should update the PublicIPAddress object status if the IP is not found and the status is already initialized", func() {
			pubipUtils.EXPECT().GetByName(ctx, azurePublicIPAddressName).Return(nil, nil)
			c.EXPECT().Get(ctx, client.ObjectKey{Namespace: pubipWithStatus.Namespace, Name: pubipWithStatus.Name}, pubipWithStatus).Return(nil)
			sw.EXPECT().Update(ctx, pubip).Return(nil)

			requeueAfter, removeFinalizer, err := actuator.CreateOrUpdate(ctx, pubipWithStatus)
			Expect(err).NotTo(HaveOccurred())
			Expect(requeueAfter).To(Equal(1 * time.Second))
			Expect(removeFinalizer).To(Equal(false))
		})
	})
})