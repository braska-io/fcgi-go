<?php
// move_uploaded_file and is_uploaded_file on the upload in field "f", and on
// a path that was not uploaded. The moved copy is removed again.
require __DIR__ . '/_emit.php';
fixture('app/request/move');
$tmp = $_FILES['f']['tmp_name'] ?? '';
$dest = (getenv('FIXTURE_STATE') ?: '/state') . '/upload-' . bin2hex(random_bytes(8));
$result = [
    'is_uploaded_file' => $tmp !== '' && is_uploaded_file($tmp),
    'foreign_is_uploaded_file' => is_uploaded_file('/etc/passwd'),
    'foreign_moved' => @move_uploaded_file('/etc/passwd', $dest),
    'moved' => $tmp !== '' && move_uploaded_file($tmp, $dest),
];
$result['tmp_left'] = $tmp !== '' && file_exists($tmp);
$result['sha256'] = is_file($dest) ? hash_file('sha256', $dest) : null;
@unlink($dest);
emit($result);
