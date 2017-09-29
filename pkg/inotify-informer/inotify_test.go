package inotifyinformer

import (
	"io/ioutil"
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"kubevirt.io/kubevirt/pkg/controller"
	"kubevirt.io/kubevirt/pkg/virt-handler/virtwrap/api"
)

var _ = Describe("Inotify", func() {

	Context("When watching files in a directory", func() {

		var tmpDir string
		var informer cache.SharedIndexInformer
		var stopInformer chan struct{}
		var queue workqueue.RateLimitingInterface

		TestForKeyEvent := func(expectedKey string, shouldExist bool) bool {
			// wait for key to either enter or exit the store.
			Eventually(func() bool {
				_, exists, _ := informer.GetStore().GetByKey(expectedKey)

				if shouldExist == exists {
					return true
				}
				return false
			}).Should(BeTrue())

			// ensure queue item for key exists
			len := queue.Len()
			for i := len; i > 0; i-- {
				key, _ := queue.Get()
				defer queue.Done(key)
				if key == expectedKey {
					return true
				}
			}
			return false
		}

		BeforeEach(func() {
			var err error
			stopInformer = make(chan struct{})
			tmpDir, err = ioutil.TempDir("", "kubevirt")
			Expect(err).ToNot(HaveOccurred())

			// create two files
			Expect(os.Create(tmpDir + "/" + "default_testvm.some-extension")).ToNot(BeNil())
			Expect(os.Create(tmpDir + "/" + "default1_testvm1.some-extension")).ToNot(BeNil())

			queue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
			informer = cache.NewSharedIndexInformer(
				NewFileListWatchFromClient(tmpDir),
				&api.Domain{},
				0,
				cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

			informer.AddEventHandler(controller.NewResourceEventHandlerFuncsForWorkqueue(queue))
			go informer.Run(stopInformer)
			Expect(cache.WaitForCacheSync(stopInformer, informer.HasSynced)).To(BeTrue())

		})

		It("should update the cache with all files in the directory", func() {
			Expect(informer.GetStore().ListKeys()).To(HaveLen(2))
			_, exists, _ := informer.GetStore().GetByKey("default/testvm")
			Expect(exists).To(BeTrue())
			_, exists, _ = informer.GetStore().GetByKey("default1/testvm1")
			Expect(exists).To(BeTrue())
		})

		It("should detect multiple creations and deletions", func() {
			num := 5
			key := "default2/testvm2"
			fileName := tmpDir + "/" + "default2_testvm2.some-extension"

			for i := 0; i < num; i++ {
				Expect(os.Create(fileName)).ToNot(BeNil())
				Expect(TestForKeyEvent(key, true)).To(Equal(true))

				Expect(os.Remove(fileName)).To(Succeed())
				Expect(TestForKeyEvent(key, false)).To(Equal(true))
			}

		})

		Context("and something goes wrong", func() {
			It("should notify and abort when listing files", func() {
				lw := NewFileListWatchFromClient(tmpDir)
				// Deleting the watch directory should have some impact
				Expect(os.RemoveAll(tmpDir)).To(Succeed())
				_, err := lw.List(v1.ListOptions{})
				Expect(err).To(HaveOccurred())
			})
			It("should ignore invalid file content", func() {
				lw := NewFileListWatchFromClient(tmpDir)
				_, err := lw.List(v1.ListOptions{})
				Expect(err).ToNot(HaveOccurred())

				i, err := lw.Watch(v1.ListOptions{})
				Expect(err).ToNot(HaveOccurred())
				defer i.Stop()

				// Adding files in wrong formats should have an impact
				// TODO should we just ignore them?
				Expect(os.Create(tmpDir + "/" + "test.some-extension")).ToNot(BeNil())

				// No event should be received
				Consistently(i.ResultChan()).ShouldNot(Receive())
			})
		})

		AfterEach(func() {
			close(stopInformer)
			os.RemoveAll(tmpDir)
		})

	})
})
