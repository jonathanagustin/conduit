#!/usr/bin/env ruby
# frozen_string_literal: true

require "digest"
require_relative "lib/checked_json"
require "rubygems/package"
require "time"
require "zlib"

abort "usage: generate-helm-sbom.rb CHART.tgz OUTPUT.json NAME VERSION" unless ARGV.length == 4

archive, output, name, version = ARGV
archive_digest = Digest::SHA256.file(archive).hexdigest
files = []

Zlib::GzipReader.open(archive) do |gzip|
  Gem::Package::TarReader.new(gzip) do |tar|
    tar.each do |entry|
      next unless entry.file?

      contents = entry.read
      files << {
        "fileName" => "./#{entry.full_name}",
        "SPDXID" => "SPDXRef-File-#{Digest::SHA256.hexdigest(entry.full_name)}",
        "checksums" => [
          {"algorithm" => "SHA1", "checksumValue" => Digest::SHA1.hexdigest(contents)},
          {"algorithm" => "SHA256", "checksumValue" => Digest::SHA256.hexdigest(contents)}
        ],
        "licenseConcluded" => "NOASSERTION",
        "copyrightText" => "NOASSERTION"
      }
    end
  end
end

files.sort_by! { |file| file.fetch("fileName") }
verification_code = Digest::SHA1.hexdigest(
  files.map { |file| file.fetch("checksums").find { |item| item.fetch("algorithm") == "SHA1" }.fetch("checksumValue") }.sort.join
)
package_id = "SPDXRef-Package-#{name}"
created = ENV.fetch("SOURCE_DATE_EPOCH", Time.now.to_i.to_s)
created_at = Time.at(Integer(created), in: "+00:00").iso8601

document = {
  "spdxVersion" => "SPDX-2.3",
  "dataLicense" => "CC0-1.0",
  "SPDXID" => "SPDXRef-DOCUMENT",
  "name" => "#{name}-#{version}",
  "documentNamespace" => "https://github.com/jonathanagustin/conduit/sbom/helm/#{name}/#{version}/#{archive_digest}",
  "creationInfo" => {
    "created" => created_at,
    "creators" => ["Tool: platform-generate-helm-sbom"]
  },
  "packages" => [{
    "name" => name,
    "SPDXID" => package_id,
    "versionInfo" => version,
    "downloadLocation" => "NOASSERTION",
    "filesAnalyzed" => true,
    "packageVerificationCode" => {"packageVerificationCodeValue" => verification_code},
    "checksums" => [{"algorithm" => "SHA256", "checksumValue" => archive_digest}],
    "licenseConcluded" => "NOASSERTION",
    "licenseDeclared" => "NOASSERTION",
    "copyrightText" => "NOASSERTION"
  }],
  "files" => files,
  "relationships" => [
    {"spdxElementId" => "SPDXRef-DOCUMENT", "relationshipType" => "DESCRIBES", "relatedSpdxElement" => package_id},
    *files.map do |file|
      {"spdxElementId" => package_id, "relationshipType" => "CONTAINS", "relatedSpdxElement" => file.fetch("SPDXID")}
    end
  ]
}

File.write(output, JSON.pretty_generate(document) + "\n")
