#!/usr/bin/env ruby
# frozen_string_literal: true

require "minitest/autorun"
require "yaml"
require_relative "lib/zarf_runtime_values"

class ZarfTracingConditionTest < Minitest::Test
  EXPORT = <<~TEXT
    {{ if .Values.TRACING_OTLP_HTTP_ENDPOINT }}
    endpoint: {{ .Values.TRACING_OTLP_HTTP_ENDPOINT | toString | toJson }}
    port: {{ .Values.TRACING_COLLECTOR_PORT }}
    {{ if and .Values.TRACING_OTLP_HTTP_ENDPOINT .Values.TRACING_CA_CONFIGMAP }}
    ca: {{ .Values.TRACING_CA_CONFIGMAP | toString | toJson }}
    {{ end }}
    {{ end }}
  TEXT
  TEMPLATE = '{{ if eq (.Values.UDS_METRICS | toString) "true" }}' + "metrics\n" + EXPORT + '{{ else }}' + "plain\n" + EXPORT + '{{ end }}'

  def values(metrics: false, endpoint: "", ca: "")
    {"UDS_METRICS" => metrics, "TRACING_OTLP_HTTP_ENDPOINT" => endpoint,
     "TRACING_CA_CONFIGMAP" => ca, "TRACING_COLLECTOR_PORT" => 4318}
  end

  def test_disabled_export_has_no_endpoint_port_or_ca_in_either_metrics_branch
    [false, true].each do |metrics|
      output = ConduitZarfRuntimeValues.render(TEMPLATE, values(metrics: metrics))
      assert_equal(metrics ? "metrics" : "plain", output.strip)
    end
  end

  def test_enabled_nested_ca_and_numeric_port_in_each_metrics_branch
    [false, true].each do |metrics|
      ["", "collector-ca"].each do |ca|
        output = ConduitZarfRuntimeValues.render(TEMPLATE, values(metrics: metrics, endpoint: "https://collector.test/v1/traces", ca: ca))
        heading, body = output.lstrip.split("\n", 2)
        assert_equal(metrics ? "metrics" : "plain", heading)
        expected = {"endpoint" => "https://collector.test/v1/traces", "port" => 4318}
        expected["ca"] = ca unless ca.empty?
        assert_equal expected, YAML.safe_load(body)
      end
    end
  end

  def test_shipped_uds_policy_has_only_bounded_opt_in_collector_egress
    root = File.expand_path("..", __dir__)
    source = File.read(File.join(root,"deploy/zarf/manifests/uds-package.yaml"))
    defaults = YAML.safe_load_file(File.join(root,"deploy/zarf/values.yaml"))
    [false,true].each do |metrics|
      [false,true].each do |enabled|
        supplied=defaults.merge("UDS_METRICS"=>metrics,"TRACING_OTLP_HTTP_ENDPOINT"=>enabled ? "https://collector.test/v1/traces" : "","TRACING_COLLECTOR_NAMESPACE"=>"observability","TRACING_COLLECTOR_NAME_LABEL"=>"collector","TRACING_COLLECTOR_PORT"=>4318)
        package=YAML.safe_load(ConduitZarfRuntimeValues.render(source,supplied))
        rules=package.fetch("spec").fetch("network").fetch("allow").select{|rule|rule.fetch("description","").include?("configured trace collector")}
        assert_equal(enabled ? 2 : 0,rules.length)
        rules.each do |rule|
          assert_equal "Egress",rule.fetch("direction")
          assert_equal "observability",rule.fetch("remoteNamespace")
          assert_equal({"app.kubernetes.io/name"=>"collector"},rule.fetch("remoteSelector"))
          assert_equal 4318,rule.fetch("port")
          assert_includes ["platform-delivery","platform-spoke-agent"],rule.fetch("selector").fetch("app.kubernetes.io/name")
          refute rule.key?("remoteHost")
          refute rule.key?("remoteGenerated")
        end
      end
    end
  end

  def test_unknown_tokens_and_conditions_are_rejected_even_in_inactive_branches
    ["{{ unknown }}", "{{ .Values.UNKNOWN | toString | toJson }}", "{{ if .Values.UNKNOWN }}ignored{{ end }}"].each do |bad|
      text = "{{ if .Values.TRACING_OTLP_HTTP_ENDPOINT }}#{bad}{{ end }}"
      assert_raises(RuntimeError, KeyError) { ConduitZarfRuntimeValues.render(text, values) }
    end
  end

  def test_unbalanced_or_duplicate_branches_are_rejected
    ["{{ else }}", "{{ end }}", "{{ if .Values.TRACING_CA_CONFIGMAP }}", "{{ if .Values.TRACING_CA_CONFIGMAP }}{{ else }}{{ else }}{{ end }}"].each do |text|
      assert_raises(RuntimeError) { ConduitZarfRuntimeValues.render(text, values) }
    end
  end

  def test_numeric_port_cannot_be_a_quoted_or_out_of_range_value
    ["4318; ignored", "4.318", "04318", 0, 65_536].each do |port|
      assert_raises(RuntimeError) { ConduitZarfRuntimeValues.render("{{ .Values.TRACING_COLLECTOR_PORT }}", values.merge("TRACING_COLLECTOR_PORT" => port)) }
    end
  end

  def test_flat_parity_values_restore_native_numeric_types
    flat=values.merge("TRACING_COLLECTOR_PORT"=>"4318","TRACING_SAMPLE_RATIO"=>"0.25")
    nested=ConduitZarfRuntimeValues.nested(flat,[])
    assert_equal 4318,nested.fetch("TRACING_COLLECTOR_PORT")
    assert_equal 0.25,nested.fetch("TRACING_SAMPLE_RATIO")
    [-0.1,1.1,"NaN","Infinity"].each do |ratio|
      assert_raises(RuntimeError,ArgumentError){ConduitZarfRuntimeValues.nested(flat.merge("TRACING_SAMPLE_RATIO"=>ratio),[])}
    end
  end
end
