#
# This is an example VCL file for Varnish.
#
# This example caches all the response from request that explicitely asked for caching
#
# See the VCL chapters in the Users Guide at https://www.varnish-cache.org/docs/
# and https://www.varnish-cache.org/trac/wiki/VCLExamples for more examples.

# Marker to tell the VCL compiler that this VCL has been adapted to the
# new 4.0 format.
vcl 4.0;

# Default backend definition. Set this to point to your content server.
backend default {
    .host = "127.0.0.1";
    .port = "8086";
}

sub vcl_backend_response {
    # Override TTL if the request is cacheable and Cache-Control was not set
    if (beresp.http.Cache-Control ~ "") {
        set beresp.ttl = 0s;
        if (bereq.http.X-Force-Cache-Control ~ "max-age") {
            set beresp.ttl = 1w;
        }
    }
}
