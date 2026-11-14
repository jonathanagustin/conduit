#!/usr/bin/env ruby
# frozen_string_literal: true
require "tmpdir"
require_relative "lib/safe_yaml"
require_relative "lib/checked_json"
require "stringio"

{"2.8.9" => false, "2.9.0" => true, "2.19.8" => true, "2.19.9" => false, "2.20.0" => false}.each do |version, affected|
  raise "JSON advisory boundary changed: #{version}" unless ConduitJSONRuntime.affected_version?(version) == affected
end
stream = StringIO.new
JSON.dump({"boundary" => "x" * 16_384}, stream)
raise "JSON streaming boundary failed" unless JSON.parse(stream.string)["boundary"] == "x" * 16_384


class DeserializationCanary
  def init_with(_coder)
    raise "arbitrary object initialization ran"
  end
end

text = "---\nname: example\ndate: 2026-11-14\ntime: 2026-11-14T12:00:00Z\nvalues: &values [1, true, null]\ncopy: *values\n---\nnull\n---\nhello\n"
documents = ConduitSafeYAML.stream(text)
raise "stream lost values" unless documents.length == 3 && documents[1].nil? && documents[2] == "hello"
raise "anchor changed" unless documents[0]["values"] == [1, true, nil] && documents[0]["copy"] == documents[0]["values"]
raise "timestamp changed" unless documents[0]["date"].is_a?(Date) && documents[0]["time"].is_a?(Time)
raise "empty stream changed" unless ConduitSafeYAML.stream("") == []

Dir.mktmpdir do |directory|
  path = File.join(directory, "fixture.yaml")
  ["--- !ruby/object:DeserializationCanary {}\n", "--- !ruby/symbol unexpected\n"].each do |payload|
    File.write(path, payload)
    [-> { ConduitSafeYAML.stream(payload) }, -> { ConduitSafeYAML.file(path) }].each do |load|
      begin
        load.call
      rescue Psych::DisallowedClass
        next
      end
      raise "unsafe tag accepted"
    end
  end
  File.write(path, text.split("---\nnull").first)
  raise "file decoding changed" unless ConduitSafeYAML.file(path) == documents.first
end
puts "restricted YAML stream and file loading passed"
