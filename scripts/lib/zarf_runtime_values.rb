# frozen_string_literal: true

require "base64"
require "json"
require "yaml"

# Deliberately bounded emulation for the parity gate. Native Zarf rendering is
# checked separately; an added template expression must never pass through as
# though this helper had evaluated it.
module ConduitZarfRuntimeValues
  def self.definition(directory, constants, version = "0.0.0")
    values = constants.merge("PLATFORM_VERSION" => version)
    text = File.read(File.join(directory, "zarf.tpl.yaml")).gsub(/\[\[ printf "%q" \.([A-Z_]+) \]\]/) do
      JSON.generate(values.fetch(Regexp.last_match(1)))
    end
    YAML.safe_load(text)
  end

  def self.render(text, values, constants = {}, action: false)
    validate_tokens(text, values)
    result = conditions(text, values)
    result = result.gsub(/\{\{ \.Values\.(?:rendered\.)?([A-Z0-9_]+) \| toString \| (b64enc|toJson) \}\}/) do
      value = values.fetch(Regexp.last_match(1)).to_s
      Regexp.last_match(2) == "b64enc" ? Base64.strict_encode64(value) : JSON.generate(value)
    end
    result = result.gsub(/\{\{ \.Values\.rendered\.([A-Z0-9_]+) \}\}/) { values.fetch(Regexp.last_match(1)) }
    result = result.gsub(/\{\{ \.Values\.UDS_METRICS \}\}/) { values.fetch("UDS_METRICS").to_s }
    result = result.gsub(/\{\{ \.Values\.TRACING_COLLECTOR_PORT \}\}/) do
      collector_port(values.fetch("TRACING_COLLECTOR_PORT")).to_s
    end
    result = result.gsub(/\{\{ index \.Pkg\.Metadata\.Annotations "conduit.dev\/(hub-image|spoke-agent-image)" \| toString \| toJson \}\}/) do
      JSON.generate(constants.fetch(Regexp.last_match(1) == "hub-image" ? "HUB_IMAGE" : "SPOKE_AGENT_IMAGE"))
    end
    result = result.gsub(/\{\{ printf "%s(\/v1\/auth\/callback|\/console\/)" \.Values\.CONSOLE_PUBLIC_URL \| toJson \}\}/) do
      JSON.generate(values.fetch("CONSOLE_PUBLIC_URL").to_s + Regexp.last_match(1))
    end
    raise "unhandled Zarf #{action ? 'action' : 'manifest'} template" if result.include?("{{")
    result
  end

  def self.validate_tokens(text, values)
    patterns = [
      /\A\{\{\s*(?:if\s+[^{}]+?|else|end)\s*\}\}\z/,
      /\A\{\{ \.Values\.(?:rendered\.)?[A-Z0-9_]+ \| toString \| (?:b64enc|toJson) \}\}\z/,
      /\A\{\{ \.Values\.rendered\.[A-Z0-9_]+ \}\}\z/,
      /\A\{\{ \.Values\.(?:UDS_METRICS|TRACING_COLLECTOR_PORT) \}\}\z/,
      /\A\{\{ index \.Pkg\.Metadata\.Annotations "conduit.dev\/(?:hub-image|spoke-agent-image)" \| toString \| toJson \}\}\z/,
      /\A\{\{ printf "%s(?:\/v1\/auth\/callback|\/console\/)" \.Values\.CONSOLE_PUBLIC_URL \| toJson \}\}\z/
    ]
    text.scan(/\{\{.*?\}\}/m).each do |token|
      raise "unsupported template token" unless patterns.any? { |pattern| pattern.match?(token) }
      token.scan(/\.Values\.(?:rendered\.)?([A-Z0-9_]+)/).each { |name| values.fetch(name.first) }
    end
  end

  # A stack is necessary: tracing conditionals nest inside the metrics branch.
  # Only the expressions actually used by the package are admitted, including
  # in inactive branches. Unknown syntax must never be silently dropped.
  def self.conditions(text, values)
    result = +""
    stack = []
    active = true
    offset = 0
    text.to_enum(:scan, /\{\{\s*(if\s+([^{}]+?)|else|end)\s*\}\}/).each do
      match = Regexp.last_match
      result << text[offset...match.begin(0)] if active
      tag = match[1].strip
      if tag.start_with?("if ")
        condition = condition_value(match[2].strip, values)
        stack << {parent: active, condition: condition, alternate: false}
        active &&= condition
      elsif tag == "else"
        raise "unmatched or duplicate template else" if stack.empty? || stack.last[:alternate]
        frame = stack.last
        frame[:alternate] = true
        active = frame[:parent] && !frame[:condition]
      else
        raise "unmatched template end" if stack.empty?
        active = stack.pop[:parent]
      end
      offset = match.end(0)
    end
    raise "unterminated template condition" unless stack.empty?
    result << text[offset..] if active
    result
  end

  def self.condition_value(expression, values)
    case expression
    when 'eq (.Values.UDS_METRICS | toString) "true"'
      values.fetch("UDS_METRICS").to_s == "true"
    when ".Values.TRACING_OTLP_HTTP_ENDPOINT", ".Values.TRACING_CA_CONFIGMAP"
      value = values.fetch(expression.delete_prefix(".Values."))
      raise "invalid tracing condition value" unless value.is_a?(String)
      !value.empty?
    when "and .Values.TRACING_OTLP_HTTP_ENDPOINT .Values.TRACING_CA_CONFIGMAP"
      condition_value(".Values.TRACING_OTLP_HTTP_ENDPOINT", values) & condition_value(".Values.TRACING_CA_CONFIGMAP", values)
    else
      raise "unsupported template condition: #{expression}"
    end
  end

  def self.nested(values, derived)
    result = values.reject { |key, _| derived.include?(key) }.merge("rendered" => values.select { |key, _| derived.include?(key) })
    metrics = values.fetch("UDS_METRICS").to_s
    raise "invalid UDS_METRICS boolean" unless %w[true false].include?(metrics)
    result["UDS_METRICS"] = metrics == "true"
    result["TRACING_COLLECTOR_PORT"] = collector_port(values.fetch("TRACING_COLLECTOR_PORT"))
    ratio = Float(values.fetch("TRACING_SAMPLE_RATIO"))
    raise "invalid tracing sample ratio" unless ratio.finite? && (0..1).cover?(ratio)
    result["TRACING_SAMPLE_RATIO"] = ratio
    result
  end

  def self.collector_port(value)
    value = Integer(value, 10) if value.is_a?(String) && value.match?(/\A[1-9][0-9]{0,4}\z/)
    raise "invalid tracing collector port" unless value.is_a?(Integer) && (1..65_535).cover?(value)
    value
  end
end
