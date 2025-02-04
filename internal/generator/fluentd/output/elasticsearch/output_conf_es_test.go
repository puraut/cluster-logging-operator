package elasticsearch_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	logging "github.com/openshift/cluster-logging-operator/apis/logging/v1"
	"github.com/openshift/cluster-logging-operator/internal/generator"
	"github.com/openshift/cluster-logging-operator/internal/generator/fluentd"
	"github.com/openshift/cluster-logging-operator/internal/generator/fluentd/output/elasticsearch"
	. "github.com/openshift/cluster-logging-operator/test/matchers"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Generating fluentd config blocks", func() {

	var (
		outputs  []logging.OutputSpec
		pipeline logging.PipelineSpec
		clfspec  logging.ClusterLogForwarderSpec
		g        generator.Generator
		secrets  map[string]*corev1.Secret
	)
	BeforeEach(func() {
		g = generator.MakeGenerator()
	})

	Context("for a secure endpoint", func() {
		BeforeEach(func() {
			outputs = []logging.OutputSpec{
				{
					Type: logging.OutputTypeElasticsearch,
					Name: "oncluster-elasticsearch",
					URL:  "https://es.svc.messaging.cluster.local:9654",
					Secret: &logging.OutputSecretSpec{
						Name: "my-es-secret",
					},
				},
			}
			pipeline = logging.PipelineSpec{
				Name:       "my-secure-pipeline",
				InputRefs:  []string{logging.InputNameApplication},
				OutputRefs: []string{"oncluster-elasticsearch"},
			}
			secrets = map[string]*corev1.Secret{
				"oncluster-elasticsearch": {
					ObjectMeta: v1.ObjectMeta{
						Name: "my-es-secret",
					},
					Data: map[string][]byte{
						"tls.key":       []byte("test-key"),
						"tls.crt":       []byte("test-crt"),
						"ca-bundle.crt": []byte("test-bundle"),
					},
				},
			}
		})

		It("should produce well formed @OUTPUT label match stanza", func() {
			pipeline.OutputRefs = append(pipeline.OutputRefs, "other-elasticsearch")
			clfspec.Pipelines = []logging.PipelineSpec{pipeline}
			clfspec.Outputs = outputs
			results, err := g.GenerateConf(fluentd.PipelineToOutputs(&clfspec, nil)...)
			Expect(err).To(BeNil())
			Expect(results).To(EqualTrimLines(`
# Copying pipeline my-secure-pipeline to outputs
<label @MY_SECURE_PIPELINE>
  <match **>
    @type copy
    <store>
      @type relabel
      @label @ONCLUSTER_ELASTICSEARCH
    </store>
    
    <store>
      @type relabel
      @label @OTHER_ELASTICSEARCH
    </store>
  </match>
</label>
`))
		})

		It("should produce well formed output label config with username/password", func() {
			secrets = map[string]*corev1.Secret{
				"oncluster-elasticsearch": {
					ObjectMeta: v1.ObjectMeta{
						Name: "my-es-secret",
					},
					Data: map[string][]byte{
						"username":      []byte("test-user"),
						"password":      []byte("test-pass"),
						"ca-bundle.crt": []byte("test-bundle"),
					},
				},
			}
			results, err := g.GenerateConf(elasticsearch.Conf(nil, secrets[outputs[0].Name], outputs[0], nil)...)
			Expect(err).To(BeNil())
			Expect(results).To(EqualTrimLines(`
<label @ONCLUSTER_ELASTICSEARCH>
  #remove structured field if present
  <filter **>
    @type record_modifier
    remove_keys structured
  </filter>
  
  #flatten labels to prevent field explosion in ES
  <filter **>
    @type record_transformer
    enable_ruby true
    <record>
      kubernetes ${!record['kubernetes'].nil? ? record['kubernetes'].merge({"flat_labels": (record['kubernetes']['labels']||{}).map{|k,v| "#{k}=#{v}"}}) : {} }
    </record>
    remove_keys $.kubernetes.labels
  </filter>
  
  <match retry_oncluster_elasticsearch>
    @type elasticsearch
    @id retry_oncluster_elasticsearch
    host es.svc.messaging.cluster.local
    port 9654
    verify_es_version_at_startup false
    scheme https
    ssl_version TLSv1_2
    user "#{File.exists?('/var/run/ocp-collector/secrets/my-es-secret/username') ? open('/var/run/ocp-collector/secrets/my-es-secret/username','r') do |f|f.read end : ''}"
    password "#{File.exists?('/var/run/ocp-collector/secrets/my-es-secret/password') ? open('/var/run/ocp-collector/secrets/my-es-secret/password','r') do |f|f.read end : ''}"
    ca_file '/var/run/ocp-collector/secrets/my-es-secret/ca-bundle.crt'
    target_index_key viaq_index_name
    id_key viaq_msg_id
    remove_keys viaq_index_name
    type_name _doc
    http_backend typhoeus
    write_operation create
    reload_connections 'true'
    # https://github.com/uken/fluent-plugin-elasticsearch#reload-after
    reload_after '200'
    # https://github.com/uken/fluent-plugin-elasticsearch#sniffer-class-name
    sniffer_class_name 'Fluent::Plugin::ElasticsearchSimpleSniffer'
    reload_on_failure false
    # 2 ^ 31
    request_timeout 2147483648
    <buffer>
      @type file
      path '/var/lib/fluentd/retry_oncluster_elasticsearch'
      flush_mode interval
      flush_interval 1s
      flush_thread_count 2
      retry_type exponential_backoff
      retry_wait 1s
      retry_max_interval 60s
      retry_timeout 60m
      queued_chunks_limit_size "#{ENV['BUFFER_QUEUE_LIMIT'] || '32'}"
      total_limit_size "#{ENV['TOTAL_LIMIT_SIZE'] || '8589934592'}"
      chunk_limit_size "#{ENV['BUFFER_SIZE_LIMIT'] || '8m'}"
      overflow_action block
    </buffer>
  </match>
  
  <match **>
    @type elasticsearch
    @id oncluster_elasticsearch
    host es.svc.messaging.cluster.local
    port 9654
    verify_es_version_at_startup false
    scheme https
    ssl_version TLSv1_2
    user "#{File.exists?('/var/run/ocp-collector/secrets/my-es-secret/username') ? open('/var/run/ocp-collector/secrets/my-es-secret/username','r') do |f|f.read end : ''}"
    password "#{File.exists?('/var/run/ocp-collector/secrets/my-es-secret/password') ? open('/var/run/ocp-collector/secrets/my-es-secret/password','r') do |f|f.read end : ''}"
    ca_file '/var/run/ocp-collector/secrets/my-es-secret/ca-bundle.crt'
    target_index_key viaq_index_name
    id_key viaq_msg_id
    remove_keys viaq_index_name
    type_name _doc
    retry_tag retry_oncluster_elasticsearch
    http_backend typhoeus
    write_operation create
    reload_connections 'true'
    # https://github.com/uken/fluent-plugin-elasticsearch#reload-after
    reload_after '200'
    # https://github.com/uken/fluent-plugin-elasticsearch#sniffer-class-name
    sniffer_class_name 'Fluent::Plugin::ElasticsearchSimpleSniffer'
    reload_on_failure false
    # 2 ^ 31
    request_timeout 2147483648
    <buffer>
      @type file
      path '/var/lib/fluentd/oncluster_elasticsearch'
      flush_mode interval
      flush_interval 1s
      flush_thread_count 2
      retry_type exponential_backoff
      retry_wait 1s
      retry_max_interval 60s
      retry_timeout 60m
      queued_chunks_limit_size "#{ENV['BUFFER_QUEUE_LIMIT'] || '32'}"
      total_limit_size "#{ENV['TOTAL_LIMIT_SIZE'] || '8589934592'}"
      chunk_limit_size "#{ENV['BUFFER_SIZE_LIMIT'] || '8m'}"
      overflow_action block
    </buffer>
  </match>
</label>`))
		})
	})

	Context("for an insecure endpoint", func() {
		BeforeEach(func() {
			pipeline.OutputRefs = []string{"other-elasticsearch"}
			outputs = []logging.OutputSpec{
				{
					Type: logging.OutputTypeElasticsearch,
					Name: "other-elasticsearch",
					URL:  "http://es.svc.messaging.cluster.local:9654",
					Secret: &logging.OutputSecretSpec{
						Name: "my-es-secret",
					},
				},
			}
			secrets = map[string]*corev1.Secret{
				"other-elasticsearch": {
					ObjectMeta: v1.ObjectMeta{
						Name: "my-es-secret",
					},
					Data: map[string][]byte{
						"username": []byte("test-user"),
						"password": []byte("test-pass"),
					},
				},
			}
		})
		It("should produce well formed output label config with username/password", func() {
			results, err := g.GenerateConf(elasticsearch.Conf(nil, secrets[outputs[0].Name], outputs[0], nil)...)
			Expect(err).To(BeNil())
			Expect(results).To(EqualTrimLines(`
<label @OTHER_ELASTICSEARCH>
  #remove structured field if present
  <filter **>
    @type record_modifier
    remove_keys structured
  </filter>
  
  #flatten labels to prevent field explosion in ES
  <filter **>
    @type record_transformer
    enable_ruby true
    <record>
      kubernetes ${!record['kubernetes'].nil? ? record['kubernetes'].merge({"flat_labels": (record['kubernetes']['labels']||{}).map{|k,v| "#{k}=#{v}"}}) : {} }
    </record>
    remove_keys $.kubernetes.labels
  </filter>
  
  <match retry_other_elasticsearch>
    @type elasticsearch
    @id retry_other_elasticsearch
    host es.svc.messaging.cluster.local
    port 9654
    verify_es_version_at_startup false
    scheme http
    user "#{File.exists?('/var/run/ocp-collector/secrets/my-es-secret/username') ? open('/var/run/ocp-collector/secrets/my-es-secret/username','r') do |f|f.read end : ''}"
    password "#{File.exists?('/var/run/ocp-collector/secrets/my-es-secret/password') ? open('/var/run/ocp-collector/secrets/my-es-secret/password','r') do |f|f.read end : ''}"
    target_index_key viaq_index_name
    id_key viaq_msg_id
    remove_keys viaq_index_name
    type_name _doc
    http_backend typhoeus
    write_operation create
    reload_connections 'true'
    # https://github.com/uken/fluent-plugin-elasticsearch#reload-after
    reload_after '200'
    # https://github.com/uken/fluent-plugin-elasticsearch#sniffer-class-name
    sniffer_class_name 'Fluent::Plugin::ElasticsearchSimpleSniffer'
    reload_on_failure false
    # 2 ^ 31
    request_timeout 2147483648
    <buffer>
      @type file
      path '/var/lib/fluentd/retry_other_elasticsearch'
      flush_mode interval
      flush_interval 1s
      flush_thread_count 2
      retry_type exponential_backoff
      retry_wait 1s
      retry_max_interval 60s
      retry_timeout 60m
      queued_chunks_limit_size "#{ENV['BUFFER_QUEUE_LIMIT'] || '32'}"
      total_limit_size "#{ENV['TOTAL_LIMIT_SIZE'] || '8589934592'}"
      chunk_limit_size "#{ENV['BUFFER_SIZE_LIMIT'] || '8m'}"
      overflow_action block
    </buffer>
  </match>
  
  <match **>
    @type elasticsearch
    @id other_elasticsearch
    host es.svc.messaging.cluster.local
    port 9654
    verify_es_version_at_startup false
    scheme http
    user "#{File.exists?('/var/run/ocp-collector/secrets/my-es-secret/username') ? open('/var/run/ocp-collector/secrets/my-es-secret/username','r') do |f|f.read end : ''}"
    password "#{File.exists?('/var/run/ocp-collector/secrets/my-es-secret/password') ? open('/var/run/ocp-collector/secrets/my-es-secret/password','r') do |f|f.read end : ''}"
    target_index_key viaq_index_name
    id_key viaq_msg_id
    remove_keys viaq_index_name
    type_name _doc
    retry_tag retry_other_elasticsearch
    http_backend typhoeus
    write_operation create
    reload_connections 'true'
    # https://github.com/uken/fluent-plugin-elasticsearch#reload-after
    reload_after '200'
    # https://github.com/uken/fluent-plugin-elasticsearch#sniffer-class-name
    sniffer_class_name 'Fluent::Plugin::ElasticsearchSimpleSniffer'
    reload_on_failure false
    # 2 ^ 31
    request_timeout 2147483648
    <buffer>
      @type file
      path '/var/lib/fluentd/other_elasticsearch'
      flush_mode interval
      flush_interval 1s
      flush_thread_count 2
      retry_type exponential_backoff
      retry_wait 1s
      retry_max_interval 60s
      retry_timeout 60m
      queued_chunks_limit_size "#{ENV['BUFFER_QUEUE_LIMIT'] || '32'}"
      total_limit_size "#{ENV['TOTAL_LIMIT_SIZE'] || '8589934592'}"
      chunk_limit_size "#{ENV['BUFFER_SIZE_LIMIT'] || '8m'}"
      overflow_action block
    </buffer>
  </match>
</label>
`))
		})
	})
})
