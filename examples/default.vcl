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
    .host = "195.157.5.51";
    .port = "8086";
}

sub vcl_recv {
    # Only caches the queries explicitely described as cacheable
    if (req.http.X-Force-Cache-Control && req.http.X-Force-Cache-Control !~ "no-cache") {
        return (hash);
    }
    return (pipe);
}

sub vcl_backend_response {
    set beresp.ttl = 24h;
}
