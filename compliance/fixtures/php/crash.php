<?php
require __DIR__ . '/_fixture.php';
$q = fixture('crash');
echo pattern((int) ($q['after_bytes'] ?? 0));
if (($q['flush'] ?? '') === '1') {
    flush();
}
die_by($q['signal'] ?? 'SEGV');
