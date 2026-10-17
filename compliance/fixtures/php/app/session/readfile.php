<?php
// Writes a file, then serves it with readfile() as the manual's download example does.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/readfile');

$size = (int) ($q['size'] ?? 128);
$path = tempnam(getenv('FIXTURE_STATE') ?: sys_get_temp_dir(), 'readfile');
if (($q['kind'] ?? 'ascii') === 'ascii') {
    // Bytes 0x00..0x7f repeating: exact in a YAML expectation.
    $data = '';
    for ($i = 0; $i < $size; $i++) {
        $data .= chr($i % 128);
    }
} else {
    $marker = 'END';
    $data = str_repeat('r', $size - strlen($marker)) . $marker;
}
file_put_contents($path, $data);
header('Content-Description: File Transfer');
header('Content-Type: application/octet-stream');
header('Content-Disposition: attachment; filename="fixture.bin"');
header('Expires: 0');
header('Cache-Control: must-revalidate');
header('Pragma: public');
header('Content-Length: ' . filesize($path));
readfile($path);
unlink($path);
