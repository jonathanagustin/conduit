# frozen_string_literal: true

require "json"
require "rubygems/version"

module ConduitJSONRuntime
  def self.affected_version?(version)
    parsed = Gem::Version.new(version)
    parsed >= Gem::Version.new("2.9.0") && parsed < Gem::Version.new("2.19.9")
  end
end

abort "Ruby json #{JSON::VERSION} is affected by GHSA-x2f5-4prf-w687; install json 2.19.9 or a newer patched release (see docs/security/THIRD-PARTY-TRUST-REVIEW.md)" if ConduitJSONRuntime.affected_version?(JSON::VERSION)
