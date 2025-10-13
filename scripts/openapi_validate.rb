#!/usr/bin/env ruby
# Basic OpenAPI sanity check without external tooling.

require "yaml"

spec_path = ARGV[0] || "docs/openapi.yaml"

begin
  spec = YAML.load_file(spec_path)
rescue Errno::ENOENT
  warn "spec file not found: #{spec_path}"
  exit 1
rescue Psych::SyntaxError => e
  warn "YAML syntax error: #{e.message}"
  exit 1
end

unless spec.is_a?(Hash)
  warn "spec root must be a mapping"
  exit 1
end

openapi_version = spec["openapi"]
unless openapi_version.is_a?(String) && openapi_version.start_with?("3.")
  warn "unsupported or missing openapi version: #{openapi_version.inspect}"
  exit 1
end

paths = spec["paths"]
unless paths.is_a?(Hash) && !paths.empty?
  warn "spec has no paths"
  exit 1
end

components = spec["components"]
unless components.is_a?(Hash)
  warn "spec missing components section"
  exit 1
end

puts "OpenAPI #{openapi_version} spec OK (#{paths.keys.size} paths, #{components.keys.size} component groups)."
