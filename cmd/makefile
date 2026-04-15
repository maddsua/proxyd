VERSION=0.0.0
ARCH=amd64
BUILDDIR=.build
DEBNAME=proxyd-debian-$(VERSION)-$(ARCH)
DEBBUILD=$(BUILDDIR)/$(DEBNAME)

clean:
	rm -rf $(BUILDDIR) || true

build-debian:
	mkdir -p $(DEBBUILD)
	cp -r ./DEBIAN $(DEBBUILD)
	cp -r ./etc $(DEBBUILD)
	mkdir -p $(DEBBUILD)/usr/bin
	go build -v -o $(DEBBUILD)/usr/bin/proxyd -ldflags "-s -w" .
	echo "Version: $(VERSION)" >> $(DEBBUILD)/DEBIAN/control
	echo "Architecture: $(ARCH)" >> $(DEBBUILD)/DEBIAN/control
	chmod +x $(DEBBUILD)/DEBIAN/postinst
	dpkg-deb -v --build --root-owner-group $(DEBBUILD)
