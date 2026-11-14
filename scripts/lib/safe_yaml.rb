# frozen_string_literal: true

require "yaml"
require "date"

# Psych.load_stream permits arbitrary Ruby objects. Decode each parsed document
# through the restricted loader while preserving YAML streams and local anchors.
module ConduitSafeYAML
  def self.stream(text)
    YAML.parse_stream(text).children.map do |document|
      stream = Psych::Nodes::Stream.new
      stream.children << document
      YAML.safe_load(stream.to_yaml, permitted_classes: [Date, Time], aliases: true)
    end
  end

  def self.file(path)
    YAML.safe_load_file(path, permitted_classes: [Date, Time], aliases: true)
  end
end
