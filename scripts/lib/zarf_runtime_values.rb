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
    result = text.gsub(/\{\{ if eq \(\.Values\.UDS_METRICS \| toString\) "true" \}\}(.*?)\{\{ else \}\}(.*?)\{\{ end \}\}/m) do
      values.fetch("UDS_METRICS").to_s == "true" ? Regexp.last_match(1) : Regexp.last_match(2)
    end
    result = result.gsub(/\{\{ \.Values\.(?:rendered\.)?([A-Z0-9_]+) \| toString \| (b64enc|toJson) \}\}/) do
      value = values.fetch(Regexp.last_match(1)).to_s
      Regexp.last_match(2) == "b64enc" ? Base64.strict_encode64(value) : JSON.generate(value)
    end
    result = result.gsub(/\{\{ \.Values\.rendered\.([A-Z0-9_]+) \}\}/) { values.fetch(Regexp.last_match(1)) }
    result = result.gsub(/\{\{ \.Values\.UDS_METRICS \}\}/) { values.fetch("UDS_METRICS").to_s }
    result = result.gsub(/\{\{ index \.Pkg\.Metadata\.Annotations "conduit.dev\/(hub-image|spoke-agent-image)" \| toString \| toJson \}\}/) do
      JSON.generate(constants.fetch(Regexp.last_match(1) == "hub-image" ? "HUB_IMAGE" : "SPOKE_AGENT_IMAGE"))
    end
    result = result.gsub(/\{\{ printf "%s(\/v1\/auth\/callback|\/console\/)" \.Values\.CONSOLE_PUBLIC_URL \| toJson \}\}/) do
      JSON.generate(values.fetch("CONSOLE_PUBLIC_URL").to_s + Regexp.last_match(1))
    end
    raise "unhandled Zarf #{action ? 'action' : 'manifest'} template" if result.include?("{{")
    result
  end

  def self.nested(values, derived)
    result = values.reject { |key, _| derived.include?(key) }.merge("rendered" => values.select { |key, _| derived.include?(key) })
    metrics = values.fetch("UDS_METRICS").to_s
    raise "invalid UDS_METRICS boolean" unless %w[true false].include?(metrics)
    result["UDS_METRICS"] = metrics == "true"
    result
  end
end
