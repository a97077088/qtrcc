V=QTFS
echo "version:${V}"
mkdir -p /mnt/${V}
mount -o loop qt-rootfs.img /mnt/${V}
echo "mount ok"
rm -rf /mnt/${V}/usr/share/demo/lm
cp lm /mnt/${V}/usr/share/demo
umount /mnt/${V}
rm -rf /mnt/${V}
echo "unmount ok"