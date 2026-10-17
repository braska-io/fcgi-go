<?php
// umask and file creation in the writable state directory.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/runtime/fs');
header('Content-Type: text/plain');

$dir = getenv('FIXTURE_STATE') . '/rt-fs';
if (!is_dir($dir)) {
    @mkdir($dir, 0777, true);
}

switch ($q['op'] ?? '') {
    case 'umask':
        printf('%04o', umask());
        break;
    case 'set_umask':
        // Leaves the umask changed on purpose; the manual says the SAPI
        // restores it when the request finishes.
        $old = umask(0077);
        printf('%04o,%04o', $old, umask());
        break;
    case 'create_umask077':
        umask(0077);
        $f = $dir . '/create77-' . getmypid() . '-' . bin2hex(random_bytes(4));
        file_put_contents($f, 'x');
        clearstatcache();
        printf('%04o', fileperms($f) & 0777);
        unlink($f);
        break;
    case 'tempnam':
        $f = tempnam($dir, 'rt');
        echo var_export(str_starts_with($f, $dir . '/rt'), true), ',';
        printf('%04o', fileperms($f) & 0777);
        unlink($f);
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
