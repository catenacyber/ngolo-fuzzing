#include "ngolofuzz.pb.h"
#include "src/libfuzzer/libfuzzer_macro.h"

extern "C" void  NGFuzzerTestOneInput(const uint8_t *buffer, size_t size);

DEFINE_PROTO_FUZZER(const ngolofuzz::NgoloFuzzList& input) {
    size_t size = input.ByteSizeLong();
    if (size > 0) {
        uint8_t *buffer = (uint8_t *) malloc(size);
        //printf("debugs %d: %s\n", size, input.DebugString().c_str());
        input.SerializeToArray((uint8_t *) buffer, size);
        NGFuzzerTestOneInput(buffer, size);
        free(buffer);
    }
}

