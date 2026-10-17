<?php
require __DIR__ . '/_fixture.php';
$q = fixture('slow');
$chunks = (int) ($q['chunks'] ?? 1);
$size = (int) ($q['chunk_size'] ?? 1);
for ($i = 0; $i < $chunks; $i++) {
    usleep((int) ($q['drip_ms'] ?? 0) * 1000);
    echo str_repeat('.', $size);
    flush();
}
